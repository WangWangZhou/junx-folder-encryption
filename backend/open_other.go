//go:build !windows

package backend

import (
	"fmt"
	"os/exec"
)

func openWithDefaultApp(path string) error {
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开路径失败: %w", err)
	}
	return nil
}

func openContainingFolder(path string) error {
	return openWithDefaultApp(path)
}
