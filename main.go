package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"folderhide/backend"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// 注册事件，绑定生成器会为它们生成对应的 TypeScript 声明。
	application.RegisterEvent[string]("folder-log")
	application.RegisterEvent[backend.JunxProgress]("folder-progress")
	application.RegisterEvent[backend.DropFiles]("files-dropped")
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

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "文件夹隐藏工具 v1.0.0",
		Width:            414,
		Height:           400,
		MinWidth:         380,
		MinHeight:        360,
		BackgroundColour: application.NewRGB(240, 240, 240),
		URL:              "/",
		// 类似播放器的"总在最前"：默认置顶，前端可用图钉按钮切换。
		AlwaysOnTop: true,
		// 允许从资源管理器把文件/文件夹拖进窗口，由前端按落点填入对应输入框。
		EnableFileDrop: true,
	})

	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		if len(files) == 0 {
			return
		}
		target := ""
		if details := event.Context().DropTargetDetails(); details != nil {
			target = details.ElementID
		}
		app.Event.Emit("files-dropped", backend.DropFiles{Paths: files, Target: target})
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
