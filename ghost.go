package main

import (
	"context"
	"math/rand"
	"time"
)

// Ghost represents one ghost.
type Ghost struct {
	ID    string
	X, Y  int
	Dir   Direction
	State GhostState
	Speed time.Duration

	updateCh  chan<- GhostUpdate  // Send position/state to game
	commandCh <-chan GhostCommand // Receive commands from game

	lua *LuaManager // may be nil; falls back to built-in AI
}

// Direction and State enums
type Direction int

const (
	Up Direction = iota
	Down
	Left
	Right
)

type GhostState int

const (
	Normal GhostState = iota
	Scared
	Eaten
)

// Messages passed via channels
type GhostUpdate struct {
	ID    string
	X, Y  int
	State GhostState
	Dir   Direction
}

type GhostCommand struct {
	Type string // "tick", "scare", "reset", etc.
	PacX int    // Player position (for chasing)
	PacY int
}

// Run starts the ghost's independent goroutine. It exits when ctx is cancelled.
func (g *Ghost) Run(ctx context.Context) {
	ticker := time.NewTicker(g.Speed)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case cmd := <-g.commandCh:
			g.handleCommand(cmd)
			// Speed may have changed; restart the ticker.
			ticker.Reset(g.Speed)

		case <-ticker.C:
			g.move()
			select {
			case g.updateCh <- GhostUpdate{
				ID:    g.ID,
				X:     g.X,
				Y:     g.Y,
				State: g.State,
				Dir:   g.Dir,
			}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Simple movement with basic AI
func (g *Ghost) move() {
	if g.State == Eaten {
		return
	}

	switch g.State {
	case Scared:
		if !g.luaMove("scared") {
			g.randomMove()
		}
	case Normal:
		if !g.luaMove("normal") {
			g.chaseMove()
		}
	default:
		g.randomMove()
	}
}

// canEnter is true when (x, y) is a tile the ghost is allowed to occupy.
// Scared ghosts no-clip through walls (panic teleport); every other state
// respects the maze.
func (g *Ghost) canEnter(x, y int) bool {
	if g.State == Scared {
		return x >= 0 && y >= 0 && x < mazeCols && y < mazeRows
	}
	return !isWall(x, y)
}

// luaMove asks the Lua script for a direction and applies it if the tile is
// reachable. Returns true if Lua handled the move, false to fall back to Go AI.
func (g *Ghost) luaMove(scriptKey string) bool {
	if g.lua == nil {
		return false
	}
	dir, ok := g.lua.RunThink(scriptKey, g.ID, g.X, g.Y, int(g.State), 0)
	if !ok {
		return false
	}
	nx, ny := g.X, g.Y
	switch dir {
	case "up":
		ny--
	case "down":
		ny++
	case "left":
		nx--
	case "right":
		nx++
	default:
		return true // "none" or unknown: Lua handled it, ghost just stays put
	}
	if g.canEnter(nx, ny) {
		g.X, g.Y = nx, ny
		switch dir {
		case "up":
			g.Dir = Up
		case "down":
			g.Dir = Down
		case "left":
			g.Dir = Left
		case "right":
			g.Dir = Right
		}
	}
	return true
}

func (g *Ghost) chaseMove() {
	// Placeholder chase: bias rightward toward x=15. Future work: read player
	// position from the engine and do real pathfinding.
	if rand.Float32() < 0.7 && g.X < 15 && g.canEnter(g.X+1, g.Y) {
		g.X++
		return
	}
	g.randomMove()
}

// randomMove tries up to four random directions before giving up for this
// tick, so a ghost cornered against walls doesn't freeze the moment its first
// pick happens to be blocked.
func (g *Ghost) randomMove() {
	order := []int{0, 1, 2, 3}
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, d := range order {
		nx, ny := g.X, g.Y
		switch d {
		case 0:
			ny--
		case 1:
			ny++
		case 2:
			nx--
		case 3:
			nx++
		}
		if g.canEnter(nx, ny) {
			g.X, g.Y = nx, ny
			return
		}
	}
}

func (g *Ghost) handleCommand(cmd GhostCommand) {
	switch cmd.Type {
	case "scare":
		g.State = Scared
		g.Speed = 150 * time.Millisecond // slower when scared
	case "reset":
		g.State = Normal
		g.Speed = 100 * time.Millisecond
	case "eat":
		g.State = Eaten
		g.Speed = 80 * time.Millisecond // faster when retreating as eyes
	}
}
