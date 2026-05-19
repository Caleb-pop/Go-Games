package main

import (
	"context"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GameEngine is the Wails-bound type. It owns the ghost goroutines, the
// channels that connect them to the game loop, and the loop itself. Ghost
// updates are forwarded to the frontend as Wails runtime events.
type GameEngine struct {
	ctx context.Context

	updateCh  chan GhostUpdate
	commandCh chan GhostCommand

	mu     sync.RWMutex
	pacX   int
	pacY   int
	ghosts map[string]*Ghost

	cancel context.CancelFunc
	loopWG sync.WaitGroup
}

func NewGameEngine() *GameEngine {
	return &GameEngine{
		updateCh:  make(chan GhostUpdate, 32),
		commandCh: make(chan GhostCommand, 32),
		ghosts:    make(map[string]*Ghost),
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

	spot := &Ghost{
		ID:        "Spot",
		X:         13,
		Y:         11,
		Speed:     150 * time.Millisecond,
		updateCh:  e.updateCh,
		commandCh: e.commandCh,
	}
	e.ghosts[spot.ID] = spot
	go spot.Run(loopCtx)

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

// ScareGhosts is called when the player eats a power pellet.
func (e *GameEngine) ScareGhosts() {
	select {
	case e.commandCh <- GhostCommand{Type: "scare"}:
	default:
	}
}

// ResetGhosts returns ghosts to normal state.
func (e *GameEngine) ResetGhosts() {
	select {
	case e.commandCh <- GhostCommand{Type: "reset"}:
	default:
	}
}
