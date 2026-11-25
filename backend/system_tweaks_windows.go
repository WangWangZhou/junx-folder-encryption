//go:build windows

package backend

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	explorerAdvancedKey   = `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`
	recentDocsKey         = `Software\Microsoft\Windows\CurrentVersion\Explorer\RecentDocs`
	hideDesktopIconsRoot  = `Software\Microsoft\Windows\CurrentVersion\Explorer\HideDesktopIcons`
	shellIconsKeyHKLM     = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\Shell Icons`
	blankShortcutArrowRes = `%windir%\System32\imageres.dll,-1015`
)

// desktopIconCLSIDs 是“此电脑/用户的文件/网络/回收站/控制面板”的桌面图标 CLSID。
// HideDesktopIcons 下的值 1 表示隐藏、0 表示显示。
var desktopIconCLSIDs = []string{
	"{20D04FE0-3AEA-1069-A2D8-08002B30309D}", // 此电脑
	"{59031a47-3f72-44a7-89c5-5595fe6b30ee}", // 用户的文件
	"{F02C1A0D-BE21-4350-88B0-7367FC96EF3C}", // 网络
	"{645FF040-5081-101B-9F08-00AA002F954E}", // 回收站
	"{5399E694-6CE5-4D6C-8FCE-1D8870FDCBA0}", // 控制面板
}

// openAdvancedWritable 打开当前用户的资源管理器高级设置键，必要时创建。
func openAdvancedWritable() (registry.Key, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, explorerAdvancedKey, registry.SET_VALUE)
	if err != nil {
		return 0, fmt.Errorf("打开资源管理器高级设置失败: %w", err)
	}
	return key, nil
}

// restartExplorer 重启资源管理器，让注册表改动立即生效。
// 会关闭用户当前打开的资源管理器窗口，调用方应先提示确认。
func restartExplorer() error {
	return restartExplorerWith(nil)
}

// restartExplorerWith 在关闭资源管理器后、重新启动前执行额外清理（例如图标缓存）。
func restartExplorerWith(beforeStart func()) error {
	// explorer 未运行时 taskkill 会返回错误，忽略即可，后续仍会启动它。
	_ = exec.Command("taskkill", "/f", "/im", "explorer.exe").Run()
	if beforeStart != nil {
		beforeStart()
	}
	if err := exec.Command("explorer.exe").Start(); err != nil {
		return fmt.Errorf("启动资源管理器失败: %w", err)
	}
	return nil
}

// clearIconCache 清理当前用户的资源管理器图标缓存，用于让去掉小箭头立即生效。
func clearIconCache() {
	local, err := os.UserCacheDir()
	if err != nil || local == "" {
		return
	}
	_ = os.Remove(filepath.Join(local, "IconCache.db"))

	explorerDir := filepath.Join(local, "Microsoft", "Windows", "Explorer")
	entries, err := os.ReadDir(explorerDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(entry.Name()), "iconcache") {
			_ = os.Remove(filepath.Join(explorerDir, entry.Name()))
		}
	}
}

// clearRecentFiles 清除当前用户的“最近使用的文件”记录（不删除原文件）。
// 只处理资源管理器的 RecentDocs 与 Recent 快捷方式，不动 Jump List 固定项。
func clearRecentFiles() error {
	if err := registry.DeleteKey(registry.CURRENT_USER, recentDocsKey); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除最近文件记录失败: %w", err)
	}

	configDir, err := os.UserConfigDir()
	if err == nil && configDir != "" {
		recentDir := filepath.Join(configDir, "Microsoft", "Windows", "Recent")
		entries, readErr := os.ReadDir(recentDir)
		if readErr == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if strings.EqualFold(name, "desktop.ini") {
					continue
				}
				_ = os.Remove(filepath.Join(recentDir, name))
			}
		}
	}
	return restartExplorer()
}

// showDesktopIcons 在桌面显示此电脑、控制面板、网络、用户文件与回收站。
func showDesktopIcons() error {
	roots := []string{
		hideDesktopIconsRoot + `\NewStartPanel`,
		hideDesktopIconsRoot + `\ClassicStartMenu`,
	}
	for _, root := range roots {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, root, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("打开桌面图标设置失败: %w", err)
		}
		for _, clsid := range desktopIconCLSIDs {
			if err := key.SetDWordValue(clsid, 0); err != nil {
				_ = key.Close()
				return fmt.Errorf("设置桌面图标显示失败: %w", err)
			}
		}
		_ = key.Close()
	}
	return restartExplorer()
}

// alignTaskbarLeft 将 Win11 任务栏/开始按钮左对齐；Win10 无影响。
func alignTaskbarLeft() error {
	key, err := openAdvancedWritable()
	if err != nil {
		return err
	}
	if err := key.SetDWordValue("TaskbarAl", 0); err != nil {
		_ = key.Close()
		return fmt.Errorf("设置任务栏左对齐失败: %w", err)
	}
	_ = key.Close()
	return restartExplorer()
}

// showHiddenFiles 显示普通隐藏文件与扩展名，但不显示受保护的系统文件。
func showHiddenFiles() error {
	key, err := openAdvancedWritable()
	if err != nil {
		return err
	}
	values := []struct {
		name  string
		value uint32
	}{
		{"Hidden", 1},
		{"HideFileExt", 0},
		{"ShowSuperHidden", 0},
	}
	for _, item := range values {
		if err := key.SetDWordValue(item.name, item.value); err != nil {
			_ = key.Close()
			return fmt.Errorf("设置文件显示选项失败: %w", err)
		}
	}
	_ = key.Close()
	return restartExplorer()
}

// isElevated 判断当前进程是否已提权。
func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// removeShortcutArrow 用空白图标覆盖快捷方式小箭头；该操作写入 HKLM，需要管理员权限。
func removeShortcutArrow() error {
	if !isElevated() {
		return errors.New("需要管理员权限，请以管理员身份运行后重试")
	}
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, shellIconsKeyHKLM, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 Shell Icons 设置失败: %w", err)
	}
	if err := key.SetStringValue("29", blankShortcutArrowRes); err != nil {
		_ = key.Close()
		return fmt.Errorf("写入快捷方式箭头覆盖失败: %w", err)
	}
	_ = key.Close()
	return restartExplorerWith(clearIconCache)
}
