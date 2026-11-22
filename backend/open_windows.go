//go:build windows

package backend

import (
	"fmt"
	"os/exec"
)

// openWithDefaultApp 使用 Windows 文件关联打开文件或文件夹。
func openWithDefaultApp(path string) error {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开路径失败: %w", err)
	}
	return nil
}

// openContainingFolder 在资源管理器中选中指定文件。
func openContainingFolder(path string) error {
	cmd := exec.Command("explorer.exe", "/select,"+path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开所在文件夹失败: %w", err)
	}
	return nil
}
