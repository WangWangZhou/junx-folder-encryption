package backend

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	startupRunKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupValueName = "FolderHideTool"
	compatFlagsKey   = `Software\Microsoft\Windows NT\CurrentVersion\App Compatibility Flags`
	runAsAdminValue  = "RUNASADMIN"
)

func startupExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取程序路径失败: %w", err)
	}
	return exe, nil
}

// readStartupState 从注册表读取开机启动与管理员运行设置。
func readStartupState() StartupState {
	state := StartupState{}
	exe, err := startupExePath()
	if err != nil {
		return state
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, startupRunKey, registry.QUERY_VALUE)
	if err == nil {
		value, _, valueErr := key.GetStringValue(startupValueName)
		key.Close()
		if valueErr == nil {
			normalized := strings.ToLower(strings.Trim(value, `"`))
			state.AutoStart = normalized == strings.ToLower(exe)
		}
	}

	flagsKey, err := registry.OpenKey(registry.CURRENT_USER, compatFlagsKey+`\`+exe, registry.QUERY_VALUE)
	if err == nil {
		_, _, valueErr := flagsKey.GetStringValue(runAsAdminValue)
		flagsKey.Close()
		state.RunAsAdmin = valueErr == nil
	}
	return state
}

// writeAutoStart 写入或删除当前用户的开机启动项。
func writeAutoStart(enabled bool) error {
	exe, err := startupExePath()
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, startupRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开启动项注册表失败: %w", err)
	}
	defer key.Close()

	if enabled {
		if err := key.SetStringValue(startupValueName, `"`+exe+`"`); err != nil {
			return fmt.Errorf("写入启动项失败: %w", err)
		}
		return nil
	}
	if err := key.DeleteValue(startupValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删除启动项失败: %w", err)
	}
	return nil
}

// writeRunAsAdmin 设置或清除程序的“以管理员身份运行”兼容标志。
func writeRunAsAdmin(enabled bool) error {
	exe, err := startupExePath()
	if err != nil {
		return err
	}
	subKey := compatFlagsKey + `\` + exe
	if enabled {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, subKey, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("打开管理员权限设置失败: %w", err)
		}
		defer key.Close()
		if err := key.SetStringValue(runAsAdminValue, ""); err != nil {
			return fmt.Errorf("写入管理员权限设置失败: %w", err)
		}
		return nil
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, subKey); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删除管理员权限设置失败: %w", err)
	}
	return nil
}
