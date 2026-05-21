package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	lua "github.com/yuin/gopher-lua"
)

// LuaManager owns a single Lua VM and exposes ghost-brain hooks to scripts.
// All calls are serialised by mu; ghost goroutines must not hold their own
// locks while calling into Lua.
type LuaManager struct {
	L       *lua.LState
	mu      sync.Mutex
	scripts map[string]*lua.LFunction

	// scriptPaths maps script key -> resolved file path, for hot-reload.
	scriptPaths map[string]string

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
		L:           L,
		scripts:     make(map[string]*lua.LFunction),
		scriptPaths: make(map[string]string),
	}
}

func (m *LuaManager) Close() {
	m.L.Close()
}

// SetPlayerPosition is called by the engine so Lua can query it via
// get_player_position().
func (m *LuaManager) SetPlayerPosition(x, y int) {
	m.mu.Lock()
	m.pacX, m.pacY = x, y
	m.mu.Unlock()
}

// resolveScriptPath tries the given path as-is first, then relative to the
// executable directory so scripts/ can live next to the .exe in production.
func resolveScriptPath(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return path
}

// LoadScript reads a Lua file and stores the named "think" function under key.
// Safe to call at any time; re-loading a key replaces the previous script.
func (m *LuaManager) LoadScript(key, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadScriptLocked(key, path)
}

// loadScriptLocked is the inner implementation; caller must hold m.mu.
func (m *LuaManager) loadScriptLocked(key, path string) error {
	path = resolveScriptPath(path)
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
	m.scriptPaths[key] = path
	return nil
}

// WatchScripts starts a goroutine that watches the scripts directory and
// hot-reloads any .lua file that changes while the game is running.
// The goroutine exits when ctx is done.
func (m *LuaManager) WatchScripts(dir string) {
	dir = resolveScriptPath(dir)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("lua watcher: could not create:", err)
		return
	}
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		fmt.Println("lua watcher: could not watch", dir, ":", err)
		return
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				if !strings.HasSuffix(event.Name, ".lua") {
					continue
				}
				m.reloadByPath(event.Name)

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("lua watcher error:", err)
			}
		}
	}()
}

// reloadByPath finds which key maps to the changed file and reloads it.
func (m *LuaManager) reloadByPath(changedPath string) {
	changedPath = filepath.Clean(changedPath)
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, p := range m.scriptPaths {
		if filepath.Clean(p) == changedPath {
			if err := m.loadScriptLocked(key, p); err != nil {
				fmt.Printf("lua hot-reload %s: %v\n", key, err)
			} else {
				fmt.Printf("lua hot-reload %s OK\n", key)
			}
			return
		}
	}
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
