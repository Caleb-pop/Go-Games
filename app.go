package main

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GameEngine is the Wails-bound type. It owns the ghost goroutines, the
// channels that connect them to the game loop, and the loop itself. Ghost
// updates are forwarded to the frontend as Wails runtime events.
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
	ghosts    map[string]*Ghost
	ghostCmds map[string]chan<- GhostCommand // send-end of each ghost's commandCh

	cancel context.CancelFunc
	loopWG sync.WaitGroup
}

func NewGameEngine() *GameEngine {
	return &GameEngine{
		updateCh:  make(chan GhostUpdate, 32),
		ghosts:    make(map[string]*Ghost),
		ghostCmds: make(map[string]chan<- GhostCommand),
		pacX:      14,
		pacY:      17,
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

// runLoop fans ghost updates out to the frontend. Scare transitions are
// driven by player events (power-pellet pickup) via ScareGhosts, not on a timer.
func (e *GameEngine) runLoop(ctx context.Context) {
	defer e.loopWG.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case update := <-e.updateCh:
			runtime.EventsEmit(e.ctx, "ghost:update", update)
		}
	}
}

// --- Bound methods callable from the frontend ---

// SetPlayerPosition lets the frontend report the player's tile coords so
// ghosts can chase. Called on every player move.
func (e *GameEngine) SetPlayerPosition(x, y int) {
	e.mu.Lock()
	e.pacX, e.pacY = x, y
	e.mu.Unlock()
}

// ScareGhosts is called when the player eats a power pellet. Unlike the
// arcade rule where every ghost flees at once, here a single random ghost is
// scared — distinct gameplay, distinct IP.
func (e *GameEngine) ScareGhosts() {
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

// ResetGhosts returns every ghost to normal state. Used by the manual R key
// and by anything else that needs to clear scared state across the board.
func (e *GameEngine) ResetGhosts() {
	for _, ch := range e.ghostCmds {
		select {
		case ch <- GhostCommand{Type: "reset"}:
		default:
		}
	}
}

// EatGhost flips a single ghost to the Eaten state. Called by the frontend
// when the player collides with a scared ghost. Unknown IDs are ignored.
func (e *GameEngine) EatGhost(id string) {
	ch, ok := e.ghostCmds[id]
	if !ok {
		return
	}
	select {
	case ch <- GhostCommand{Type: "eat"}:
	default:
	}
}
