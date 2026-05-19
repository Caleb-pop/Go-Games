package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	engine := NewGameEngine()

	err := wails.Run(&options.App{
		Title:  "MunchenLepard",
		Width:  896,
		Height: 992,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 1},
		OnStartup:        engine.Startup,
		OnShutdown:       engine.Shutdown,
		Bind: []interface{}{
			engine,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
