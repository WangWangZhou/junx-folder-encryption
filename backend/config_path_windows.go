//go:build windows

package backend

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const (
	configRegistryKey   = `Software\FolderHideTool`
	configPathValueName = "ConfigPath"
)

// readConfigPath 从注册表读取当前 config.jsonc 的完整路径。
func readConfigPath() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, configRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return "", nil
		}
		return "", fmt.Errorf("打开配置路径注册表失败: %w", err)
	}
	defer key.Close()
	path, _, err := key.GetStringValue(configPathValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return "", nil
		}
		return "", fmt.Errorf("读取配置路径失败: %w", err)
	}
	return path, nil
}

// writeConfigPath 把新的配置文件路径写入注册表。
func writeConfigPath(path string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, configRegistryKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("写入配置路径注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(configPathValueName, path); err != nil {
		return fmt.Errorf("写入配置路径失败: %w", err)
	}
	return nil
}

// clearConfigPath 清除配置路径注册表值。
func clearConfigPath() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, configRegistryKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("打开配置路径注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(configPathValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删除配置路径失败: %w", err)
	}
	return nil
}
