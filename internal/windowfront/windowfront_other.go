//go:build !windows

package windowfront

import "github.com/wailsapp/wails/v3/pkg/application"

// BringToFront 非 Windows 平台退化实现。
// macOS/Linux 对后台进程置前的限制与 Windows 不同，直接使用 Wails 自带聚焦即可。
func BringToFront(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	window.Show().Focus()
}
