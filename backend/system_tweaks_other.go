//go:build !windows

package backend

import "errors"

var errWindowsOnly = errors.New("仅支持 Windows 系统")
var errNeedAdmin = errors.New("需要管理员权限，请以管理员身份运行后重试")

func clearRecentFiles() error    { return errWindowsOnly }
func showDesktopIcons() error    { return errWindowsOnly }
func alignTaskbarLeft() error    { return errWindowsOnly }
func showHiddenFiles() error     { return errWindowsOnly }
func removeShortcutArrow() error { return errNeedAdmin }
func restartExplorer() error     { return errWindowsOnly }
func isElevated() bool           { return false }
