//go:build windows

// Package windowfront 提供跨平台的"把窗口真正带到前台"能力。
package windowfront

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// BringToFront 显示窗口并将其真正带到前台。
//
// 单实例回调（OnSecondInstanceLaunch）运行在后台 goroutine，没有用户输入上下文。
// Windows 的前台锁定规则会拒绝此类进程调用 SetForegroundWindow —— 结果是窗口虽然
// 显示了、任务栏也出现图标，但不会被激活到屏幕最前，用户看到的只是"在任务栏闪烁"。
// 托盘点击不受影响，因为它是真实的用户输入事件。
//
// 解决方式是在置前前模拟一次 ALT 键的按下与释放，向系统表明"发生了用户输入"，
// 从而解除本次调用的前台锁定。这是桌面应用（Electron/Tauri/Wails）的通用做法。
func BringToFront(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	window.Show()

	hwnd := w32.HWND(window.NativeWindow())
	if hwnd == 0 {
		// 取不到原生句柄时退化为 Wails 自带的聚焦逻辑。
		window.Focus()
		return
	}

	// 模拟 ALT 键按下/释放，解除 Windows 前台锁定。
	w32.KeybdEvent(w32.VK_MENU, 0, 0, 0)
	w32.KeybdEvent(w32.VK_MENU, 0, w32.KEYEVENTF_KEYUP, 0)
	w32.BringWindowToTop(hwnd)
	w32.SetForegroundWindow(hwnd)

	// 让 WebView2 也拿到键盘焦点（Wails 的 Focus 内部会处理 controller 状态）。
	window.Focus()
}
