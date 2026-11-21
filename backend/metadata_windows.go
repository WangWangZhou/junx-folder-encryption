//go:build windows

package backend

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// dsStorePath 返回文件夹内部 .DS_Store 的不带 \\?\ 前缀路径。
func dsStorePath(folder string) (string, error) {
	clean, err := canonicalPath(folder)
	if err != nil {
		return "", err
	}
	return joinChildPath(clean, dsStoreName), nil
}

// writeDSStore 在目标文件夹内写入 .DS_Store，并设置隐藏与系统属性。
// 文件已存在时拒绝覆盖，避免破坏真实元数据。
func writeDSStore(folder string, data []byte) error {
	child, err := dsStorePath(folder)
	if err != nil {
		return err
	}
	ptr, err := pathPtr(child)
	if err != nil {
		return err
	}
	if attrs, err := windows.GetFileAttributes(ptr); err == nil && attrs != windows.INVALID_FILE_ATTRIBUTES {
		return fmt.Errorf("%s 已存在，拒绝覆盖", dsStoreName)
	}

	ep, err := extendedPath(child)
	if err != nil {
		return err
	}
	handle, err := windows.Open(ep, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %w", dsStoreName, err)
	}
	defer windows.CloseHandle(handle)

	for len(data) > 0 {
		n, err := windows.Write(handle, data)
		if err != nil {
			return fmt.Errorf("写入 %s 失败: %w", dsStoreName, err)
		}
		if n == 0 {
			return fmt.Errorf("写入 %s 时没有进展", dsStoreName)
		}
		data = data[n:]
	}

	attrs, err := windows.GetFileAttributes(ptr)
	if err == nil && attrs != windows.INVALID_FILE_ATTRIBUTES {
		_ = windows.SetFileAttributes(ptr, attrs|windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM)
	}
	return nil
}

// readDSStore 读取目标文件夹内部的 .DS_Store。
func readDSStore(folder string) ([]byte, error) {
	child, err := dsStorePath(folder)
	if err != nil {
		return nil, err
	}
	ep, err := extendedPath(child)
	if err != nil {
		return nil, err
	}
	handle, err := windows.Open(ep, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", dsStoreName, err)
	}
	defer windows.CloseHandle(handle)

	buf := make([]byte, 4096)
	out := make([]byte, 0, len(buf))
	for {
		n, err := windows.Read(handle, buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", dsStoreName, err)
		}
		if n == 0 {
			break
		}
	}
	return out, nil
}

// removeDSStore 清除属性后删除元数据文件。
func removeDSStore(folder string) error {
	child, err := dsStorePath(folder)
	if err != nil {
		return err
	}
	ptr, err := pathPtr(child)
	if err != nil {
		return err
	}
	if attrs, err := windows.GetFileAttributes(ptr); err == nil && attrs != windows.INVALID_FILE_ATTRIBUTES {
		_ = windows.SetFileAttributes(ptr, attrs &^ (windows.FILE_ATTRIBUTE_HIDDEN | windows.FILE_ATTRIBUTE_SYSTEM))
	}
	if err := windows.DeleteFile(ptr); err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return fmt.Errorf("删除 %s 失败: %w", dsStoreName, err)
	}
	return nil
}
