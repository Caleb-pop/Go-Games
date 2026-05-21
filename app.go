package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GameState is the snapshot emitted to the frontend on every state change.
type GameState struct {
	Score    int  `json:"Score"`
	Lives    int  `json:"Lives"`
	GameOver bool `json:"GameOver"`
}

// PlayerUpdate is emitted after each successful MovePlayer call.
type PlayerUpdate struct {
	X   int `json:"X"`
	Y   int `json:"Y"`
	Dir int `json:"Dir"` // 0=right 1=left 2=up 3=down
}

// GameEngine is the Wails-bound type. It owns the ghost goroutines, the
// channels that connect them to the game loop, and the loop itself. Ghost
// updates and player updates are forwarded to the frontend as Wails runtime
// events. All simulation state (position, pellets, score, lives) lives here.
//
// Each ghost has its own command channel so that broadcasts (scare, reset)
// reach every ghost rather than being consumed by whichever goroutine wins
// the race for a shared channel.
type GameEngine struct {
	ctx context.Context

	updateCh chan GhostUpdate

	mu        sync.RWMutex
	pacX      int
	pacY      int
	pellets   [][]int8 // 0=pellet 1=wall 2=eaten 3=power-pellet-eaten
	score     int
	lives     int
	gameOver  bool
	ghosts    map[string]*Ghost
	ghostCmds map[string]chan<- GhostCommand // send-end of each ghost's commandCh

	lua *LuaManager

	cancel context.CancelFunc
	loopWG sync.WaitGroup
}

const (
	playerSpawnX = 14
	playerSpawnY = 23
)

var powerPellets = [][2]int{{2, 2}, {25, 2}, {2, 28}, {25, 28}}

func isPowerPellet(x, y int) bool {
	for _, p := range powerPellets {
		if p[0] == x && p[1] == y {
			return true
		}
	}
	return false
}

func buildPellets() [][]int8 {
	grid := make([][]int8, mazeRows)
	for y := 0; y < mazeRows; y++ {
		grid[y] = make([]int8, mazeCols)
		for x := 0; x < mazeCols; x++ {
			if mazeWalls[y][x] {
				grid[y][x] = 1
			}
		}
	}
	return grid
}

func NewGameEngine() *GameEngine {
	lm := NewLuaManager()

	// Load scripts; log but don't crash on missing files so the game still runs
	// with fallback Go AI if scripts are absent.
	if err := lm.LoadScript("normal", "scripts/ghost_normal.lua"); err != nil {
		fmt.Println("warning:", err)
	}
	if err := lm.LoadScript("scared", "scripts/ghost_scared.lua"); err != nil {
		fmt.Println("warning:", err)
	}

	return &GameEngine{
		updateCh:  make(chan GhostUpdate, 32),
		ghosts:    make(map[string]*Ghost),
		ghostCmds: make(map[string]chan<- GhostCommand),
		pacX:      playerSpawnX,
		pacY:      playerSpawnY,
		pellets:   buildPellets(),
		lives:     3,
		lua:       lm,
	}
}

// Startup is called by Wails once the frontend is ready. We capture the
// context (needed for runtime.EventsEmit) and spin up the ghost goroutines
// plus the main loop.
func (e *GameEngine) Startup(ctx context.Context) {
	e.ctx = ctx
	loopCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	spawns := []struct {
		ID   string
		X, Y int
	}{
		{"Spot", 13, 11},
		{"Tracker", 14, 11},
		{"Shadow", 15, 11},
	}
	for _, s := range spawns {
		cmdCh := make(chan GhostCommand, 4)
		g := &Ghost{
			ID:        s.ID,
			X:         s.X,
			Y:         s.Y,
			Speed:     150 * time.Millisecond,
			updateCh:  e.updateCh,
			commandCh: cmdCh,
			lua:       e.lua,
		}
		e.ghosts[g.ID] = g
		e.ghostCmds[g.ID] = cmdCh
		go g.Run(loopCtx)
	}

	e.loopWG.Add(1)
	go e.runLoop(loopCtx)
}

func (e *GameEngine) Shutdown(ctx context.Context) {
	if e.cancel != nil {
		e.cancel()
	}
	e.loopWG.Wait()
}

// runLoop fans ghost updates out to the frontend and checks ghost-player
// collisions after each ghost move.
func (e *GameEngine) runLoop(ctx context.Context) {
	defer e.loopWG.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case update := <-e.updateCh:
			runtime.EventsEmit(e.ctx, "ghost:update", update)
			e.checkGhostCollision(update)
		}
	}
}

// checkGhostCollision is called after every ghost update. Must not hold e.mu
// while calling back into ghost command channels to avoid deadlock.
func (e *GameEngine) checkGhostCollision(update GhostUpdate) {
	e.mu.Lock()
	if e.gameOver {
		e.mu.Unlock()
		return
	}
	if update.X != e.pacX || update.Y != e.pacY {
		e.mu.Unlock()
		return
	}
	switch update.State {
	case Scared:
		e.score += 200
		state := e.emitState()
		e.mu.Unlock()
		runtime.EventsEmit(e.ctx, "game:state", state)
		// Eat the ghost outside the lock.
		if ch, ok := e.ghostCmds[update.ID]; ok {
			select {
			case ch <- GhostCommand{Type: "eat"}:
			default:
			}
		}
	case Normal:
		e.loseLifeLocked()
	default:
		e.mu.Unlock()
	}
}

// loseLifeLocked decrements lives and handles respawn or game-over.
// Caller must hold e.mu; this method releases it.
func (e *GameEngine) loseLifeLocked() {
	e.lives--
	if e.lives <= 0 {
		e.gameOver = true
		state := e.emitState()
		e.mu.Unlock()
		runtime.EventsEmit(e.ctx, "game:state", state)
		return
	}
	e.pacX, e.pacY = playerSpawnX, playerSpawnY
	e.lua.SetPlayerPosition(e.pacX, e.pacY)
	state := e.emitState()
	update := PlayerUpdate{X: e.pacX, Y: e.pacY, Dir: 0}
	e.mu.Unlock()
	runtime.EventsEmit(e.ctx, "player:update", update)
	runtime.EventsEmit(e.ctx, "game:state", state)
}

// emitState snapshots the current score/lives/gameOver for event emission.
// Caller must hold e.mu.
func (e *GameEngine) emitState() GameState {
	return GameState{Score: e.score, Lives: e.lives, GameOver: e.gameOver}
}

// --- Bound methods callable from the frontend ---

// MovePlayer validates the requested direction against the maze, updates the
// player's authoritative position, handles pellet collection, and emits
// player:update + game:state events. dir: "up" "down" "left" "right".
// Returns false if the move is blocked by a wall (frontend can ignore it).
func (e *GameEngine) MovePlayer(dir string) bool {
	e.mu.Lock()

	if e.gameOver {
		e.mu.Unlock()
		return false
	}

	nx, ny := e.pacX, e.pacY
	facing := 0
	switch dir {
	case "up":
		ny--
		facing = 2
	case "down":
		ny++
		facing = 3
	case "left":
		nx--
		facing = 1
	case "right":
		nx++
		facing = 0
	default:
		e.mu.Unlock()
		return false
	}

	if isWall(nx, ny) {
		e.mu.Unlock()
		return false
	}

	e.pacX, e.pacY = nx, ny
	e.lua.SetPlayerPosition(nx, ny)

	// Pellet collection.
	var scarePellet bool
	if e.pellets[ny][nx] == 0 {
		if isPowerPellet(nx, ny) {
			e.pellets[ny][nx] = 3
			e.score += 50
			scarePellet = true
		} else {
			e.pellets[ny][nx] = 2
			e.score += 10
		}
	}

	stateSnap := e.emitState()
	playerSnap := PlayerUpdate{X: nx, Y: ny, Dir: facing}
	pelletSnap := e.copyPelletsLocked()
	e.mu.Unlock()

	runtime.EventsEmit(e.ctx, "player:update", playerSnap)
	runtime.EventsEmit(e.ctx, "game:state", stateSnap)
	runtime.EventsEmit(e.ctx, "pellet:update", pelletSnap)
	if scarePellet {
		e.scareOne()
	}
	return true
}

// GetState returns the current game state snapshot for initial frontend sync.
func (e *GameEngine) GetState() GameState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.emitState()
}

// GetPlayerPosition returns the current player tile coordinates for initial sync.
func (e *GameEngine) GetPlayerPosition() PlayerUpdate {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return PlayerUpdate{X: e.pacX, Y: e.pacY, Dir: 0}
}

// GetPellets returns the full pellet grid for initial frontend sync.
// Values: 0=pellet present, 1=wall, 2=normal pellet eaten, 3=power pellet eaten.
func (e *GameEngine) GetPellets() [][]int8 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.copyPelletsLocked()
}

// copyPelletsLocked copies the pellet grid. Caller must hold at least a read lock.
func (e *GameEngine) copyPelletsLocked() [][]int8 {
	out := make([][]int8, len(e.pellets))
	for i, row := range e.pellets {
		c := make([]int8, len(row))
		copy(c, row)
		out[i] = c
	}
	return out
}

// scareOne picks a random normal ghost and scares it.
func (e *GameEngine) scareOne() {
	ids := make([]string, 0, len(e.ghostCmds))
	for id := range e.ghostCmds {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	target := ids[rand.Intn(len(ids))]
	select {
	case e.ghostCmds[target] <- GhostCommand{Type: "scare"}:
	default:
	}
}

// ResetGhosts returns every ghost to normal state.
func (e *GameEngine) ResetGhosts() {
	for _, ch := range e.ghostCmds {
		select {
		case ch <- GhostCommand{Type: "reset"}:
		default:
		}
	}
}
