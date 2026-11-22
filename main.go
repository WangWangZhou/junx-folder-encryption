package main

import (
	"embed"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"folderhide/backend"
	"folderhide/internal/windowfront"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/trayicon.png
var trayIcon []byte

func init() {
	// 注册事件，绑定生成器会为它们生成对应的 TypeScript 声明。
	application.RegisterEvent[string]("folder-log")
	application.RegisterEvent[backend.JunxProgress]("folder-progress")
	application.RegisterEvent[backend.DropFiles]("files-dropped")
}

// trayText 保存系统托盘的标签与菜单文案。
type trayText struct {
	label string
	show  string
	quit  string
}

// trayTextFor 按界面语言返回托盘文案；未识别的语言按简体中文处理。
func trayTextFor(language string) trayText {
	switch language {
	case "en":
		return trayText{label: "Folder Hide", show: "Show Window", quit: "Quit"}
	case "ja":
		return trayText{label: "フォルダー非表示", show: "ウィンドウを表示", quit: "終了"}
	default:
		return trayText{label: "文件夹隐藏工具", show: "显示主窗口", quit: "退出"}
	}
}

// screenInfos 把 Wails 的屏幕列表转成后端校验所需的精简结构。
func screenInfos(screens []*application.Screen) []backend.ScreenInfo {
	result := make([]backend.ScreenInfo, 0, len(screens))
	for _, screen := range screens {
		if screen == nil {
			continue
		}
		result = append(result, backend.ScreenInfo{
			ID:        screen.ID,
			IsPrimary: screen.IsPrimary,
			WorkArea: backend.Rect{
				X:      screen.WorkArea.X,
				Y:      screen.WorkArea.Y,
				Width:  screen.WorkArea.Width,
				Height: screen.WorkArea.Height,
			},
		})
	}
	return result
}

func main() {
	service := backend.NewFolderService()

	// 供 OnShutdown 清理，避免托盘图标残留在通知区域。
	var systray *application.SystemTray
	// 窗口位置状态与延迟落盘定时器。
	var stateMu sync.Mutex
	var pendingState backend.WindowState
	var saveTimer *time.Timer
	// 提前声明，供 SingleInstance 回调引用；具体逻辑在窗口创建后赋值。
	var restoreAndShow func()

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
			// 托盘应用：关闭最后一个窗口后仍需继续运行。
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			// 点 X 时由 WindowClosing hook 改为隐藏，禁止框架自动退出。
			DisableQuitOnLastWindowClosed: true,
		},
		SingleInstance: &application.SingleInstanceOptions{
			// 与 build/config.yml 的 productIdentifier 一致，保证唯一。
			UniqueID: "com.junxin.fencryption",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				// 第二实例由 Wails 自动退出；这里让首实例恢复窗口。
				application.InvokeSync(func() {
					restoreAndShow()
				})
			},
		},
		OnShutdown: func() {
			// 取消未触发的延时写，立即落盘当前状态并清理托盘。
			stateMu.Lock()
			if saveTimer != nil {
				saveTimer.Stop()
			}
			final := pendingState
			stateMu.Unlock()
			if final.IsValid() {
				_ = backend.SaveWindowState(final)
			}
			if systray != nil {
				systray.Destroy()
			}
		},
	})

	// 读取上次关闭时的窗口状态，按当前屏幕布局做多屏校验与回退。
	saved := backend.LoadWindowState()
	bounds, monitorID := backend.ResolveWindowBounds(saved, screenInfos(app.Screen.GetAll()))
	initial := saved
	initial.X, initial.Y = bounds.X, bounds.Y
	initial.Width, initial.Height = bounds.Width, bounds.Height
	initial.MonitorID = monitorID
	stateMu.Lock()
	pendingState = initial
	stateMu.Unlock()

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "文件夹隐藏工具 v1.0.0",
		Width:            bounds.Width,
		Height:           bounds.Height,
		MinWidth:         backend.MinWindowWidth,
		MinHeight:        backend.MinWindowHeight,
		X:                bounds.X,
		Y:                bounds.Y,
		BackgroundColour: application.NewRGB(240, 240, 240),
		URL:              "/",
		// 类似播放器的"总在最前"：默认置顶，前端可用图钉按钮切换。
		AlwaysOnTop: true,
		// 允许从资源管理器把文件/文件夹拖进窗口，由前端按落点填入对应输入框。
		EnableFileDrop: true,
	})

	// captureState 从窗口读取当前位置与大小，并记录所在显示器。
	captureState := func() backend.WindowState {
		x, y := window.Position()
		width, height := window.Size()
		state := backend.WindowState{
			Version:   1,
			X:         x,
			Y:         y,
			Width:     width,
			Height:    height,
			MonitorID: monitorID,
		}
		if screen, err := window.GetScreen(); err == nil && screen != nil {
			state.MonitorID = screen.ID
		}
		return state
	}

	// scheduleSave 短延时批量落盘，避免拖动窗口时频繁写磁盘。
	scheduleSave := func() {
		stateMu.Lock()
		pendingState = captureState()
		if saveTimer == nil {
			saveTimer = time.AfterFunc(400*time.Millisecond, func() {
				stateMu.Lock()
				state := pendingState
				saveTimer = nil
				stateMu.Unlock()
				if state.IsValid() {
					_ = backend.SaveWindowState(state)
				}
			})
		} else {
			saveTimer.Reset(400 * time.Millisecond)
		}
		stateMu.Unlock()
	}

	// 恢复窗口到记忆位置并显示。托盘点击、托盘菜单与单实例回调共用此入口，
	// 刻意不调用 SystemTray.PositionWindow —— 后者会把窗口强制移到托盘右下角。
	restoreAndShow = func() {
		state := backend.LoadWindowState()
		if state.IsValid() {
			rect, id := backend.ResolveWindowBounds(state, screenInfos(app.Screen.GetAll()))
			window.SetBounds(application.Rect{
				X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height,
			})
			stateMu.Lock()
			pendingState.X, pendingState.Y = rect.X, rect.Y
			pendingState.Width, pendingState.Height = rect.Width, rect.Height
			pendingState.MonitorID = id
			stateMu.Unlock()
		}
		// 用 windowfront.BringToFront 而非直接 Show().Focus()：单实例回调运行在
		// 后台 goroutine，缺少用户输入上下文，Windows 前台锁定会拒绝激活，
		// 导致窗口只出现在任务栏而不弹到最前。该函数会先解除前台锁定。
		windowfront.BringToFront(window)
	}

	// 拖动与缩放时更新内存状态并延时落盘。
	window.OnWindowEvent(events.Common.WindowDidMove, func(event *application.WindowEvent) {
		scheduleSave()
	})
	window.OnWindowEvent(events.Common.WindowDidResize, func(event *application.WindowEvent) {
		scheduleSave()
	})

	// 点窗口 X 时隐藏到托盘而非退出，并在隐藏前立即保存状态。
	// app.Quit() 不经过此 hook，两个退出入口不受影响。
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		stateMu.Lock()
		pendingState = captureState()
		if saveTimer != nil {
			saveTimer.Stop()
			saveTimer = nil
		}
		final := pendingState
		stateMu.Unlock()
		if final.IsValid() {
			_ = backend.SaveWindowState(final)
		}
		window.Hide()
		e.Cancel()
	})

	// 托盘默认开启。用 OnClick 覆盖 AttachWindow 的默认切换行为，
	// 避免其内置 PositionWindow 把窗口挪到通知区域右下角。
	text := trayTextFor(service.GetConfigInfo().Language)
	menu := app.NewMenu()
	menu.Add(text.show).OnClick(func(ctx *application.Context) {
		restoreAndShow()
	})
	menu.Add(text.quit).OnClick(func(ctx *application.Context) {
		app.Quit()
	})

	systray = app.SystemTray.New()
	systray.SetIcon(trayIcon)
	systray.SetLabel(text.label)
	systray.SetMenu(menu)
	systray.OnClick(func() {
		if window.IsVisible() {
			window.Hide()
			return
		}
		restoreAndShow()
	})
	systray.AttachWindow(window)

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
