//go:build !windows

package backend

import "fmt"

var errStartupWindowsOnly = fmt.Errorf("开机启动设置仅支持 Windows")

func readStartupState() StartupState {
	return StartupState{}
}

func writeAutoStart(enabled bool) error {
	return errStartupWindowsOnly
}

func writeRunAsAdmin(enabled bool) error {
	return errStartupWindowsOnly
}
