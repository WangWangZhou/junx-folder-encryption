package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"folderhide/backend"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// 注册日志事件，绑定生成器会为它生成对应的 TypeScript 声明。
	application.RegisterEvent[string]("folder-log")
}

func main() {
	service := backend.NewFolderService()

	app := application.New(application.Options{
		Name:        "文件夹隐藏工具",
		Description: "通过重命名实现文件夹的隐藏与恢复",
		Services: []application.Service{
			application.NewService(service),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "文件夹隐藏工具 v1.2",
		Width:            414,
		Height:           319,
		MinWidth:         380,
		MinHeight:        280,
		BackgroundColour: application.NewRGB(240, 240, 240),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
