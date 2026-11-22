//go:build !windows

package backend

import "fmt"

// 非 Windows 平台不使用注册表指针，直接使用默认配置路径。
func readConfigPath() (string, error) {
	return "", nil
}

func writeConfigPath(path string) error {
	return fmt.Errorf("更改配置路径仅支持 Windows")
}

func clearConfigPath() error {
	return nil
}
