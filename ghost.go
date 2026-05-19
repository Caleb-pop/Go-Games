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

	// Very basic "AI" - chase or random
	switch g.State {
	case Scared:
		g.randomMove()
	case Normal:
		g.chaseMove() // Simplified
	default:
		g.randomMove()
	}
}

func (g *Ghost) chaseMove() {
	// In real game you'd have proper pathfinding (A*)
	// Here we just move mostly towards player (demo)
	if rand.Float32() < 0.7 {
		// Bias toward player
		if g.X < 15 {
			g.X++
		} // dummy logic
	} else {
		g.randomMove()
	}
}

func (g *Ghost) randomMove() {
	switch rand.Intn(4) {
	case 0:
		g.Y--
	case 1:
		g.Y++
	case 2:
		g.X--
	case 3:
		g.X++
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
	}
}
