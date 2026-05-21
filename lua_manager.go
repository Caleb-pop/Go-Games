package main

import (
	"fmt"
	"os"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// LuaManager owns a single Lua VM and exposes ghost-brain hooks to scripts.
// All calls are serialised by mu; ghost goroutines must not hold their own
// locks while calling into Lua.
type LuaManager struct {
	L       *lua.LState
	mu      sync.Mutex
	scripts map[string]*lua.LFunction

	// These are set before any script call so Lua globals can read them.
	pacX, pacY int
}

func NewLuaManager() *LuaManager {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	lua.OpenBase(L)
	lua.OpenMath(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	return &LuaManager{
		L:       L,
		scripts: make(map[string]*lua.LFunction),
	}
}

func (m *LuaManager) Close() {
	m.L.Close()
}

// SetPlayerPosition is called by the engine before any ghost tick so Lua can
// query it via get_player_position().
func (m *LuaManager) SetPlayerPosition(x, y int) {
	m.mu.Lock()
	m.pacX, m.pacY = x, y
	m.mu.Unlock()
}

// LoadScript reads a Lua file and stores the named "think" function under key.
// Safe to call at startup; re-loading a key replaces the previous script.
func (m *LuaManager) LoadScript(key, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("lua: read %s: %w", path, err)
	}
	if err := m.L.DoString(string(src)); err != nil {
		return fmt.Errorf("lua: exec %s: %w", path, err)
	}
	fn, ok := m.L.GetGlobal("think").(*lua.LFunction)
	if !ok {
		return fmt.Errorf("lua: %s must define a global function think(ghost_id, gx, gy, state, dt)", path)
	}
	m.scripts[key] = fn
	return nil
}

// RunThink calls scripts[key].think(ghostID, gx, gy, state, dt) and returns
// the direction string the script chose ("up", "down", "left", "right") plus
// whether to override the built-in move. Returns ok=false if no script is
// registered for key or the call fails.
func (m *LuaManager) RunThink(key, ghostID string, gx, gy, state int, dt float64) (dir string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fn, exists := m.scripts[key]
	if !exists {
		return "", false
	}

	// Expose read-only globals the script can call.
	px, py := m.pacX, m.pacY
	m.L.SetGlobal("get_player_position", m.L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(px))
		L.Push(lua.LNumber(py))
		return 2
	}))
	m.L.SetGlobal("get_maze_size", m.L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(mazeCols))
		L.Push(lua.LNumber(mazeRows))
		return 2
	}))

	err := m.L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	},
		lua.LString(ghostID),
		lua.LNumber(gx),
		lua.LNumber(gy),
		lua.LNumber(state),
		lua.LNumber(dt),
	)
	if err != nil {
		fmt.Printf("lua: %s think error: %v\n", key, err)
		return "", false
	}

	ret := m.L.Get(-1)
	m.L.Pop(1)
	if s, ok2 := ret.(lua.LString); ok2 {
		return string(s), true
	}
	return "", false
}
