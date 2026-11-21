//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// extendedPath 构造 Win32 扩展路径。
// 普通 Win32 路径会去掉末尾的句点和空格，只有加上 \\?\ 前缀才能把
// "文件夹.." 这样的名字原样交给文件系统处理。
func extendedPath(p string) (string, error) {
	canonical, err := canonicalPath(p)
	if err != nil {
		return "", err
	}
	return `\\?\` + canonical, nil
}

func pathPtr(p string) (*uint16, error) {
	ep, err := extendedPath(p)
	if err != nil {
		return nil, err
	}
	return windows.UTF16PtrFromString(ep)
}

// pathExists 用扩展路径判断条目是否存在，普通 os.Stat 无法识别尾随句点的名字。
func pathExists(p string) bool {
	ptr, err := pathPtr(p)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(ptr)
	return err == nil && attrs != windows.INVALID_FILE_ATTRIBUTES
}

// isDirectoryRaw 通过原始属性判断是否为目录。
func isDirectoryRaw(p string) bool {
	ptr, err := pathPtr(p)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil || attrs == windows.INVALID_FILE_ATTRIBUTES {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0
}

// renamePath 使用 MoveFileEx 重命名，绕过 Win32 的路径规范化。
func renamePath(from, to string) error {
	src, err := pathPtr(from)
	if err != nil {
		return err
	}
	dst, err := pathPtr(to)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(src, dst, 0); err != nil {
		return fmt.Errorf("重命名失败: %w", err)
	}
	return nil
}

// setFolderHidden 设置或清除 FILE_ATTRIBUTE_HIDDEN，同时保留其他属性。
func setFolderHidden(p string, hidden bool) error {
	ptr, err := pathPtr(p)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil || attrs == windows.INVALID_FILE_ATTRIBUTES {
		return fmt.Errorf("读取文件属性失败: %w", err)
	}
	if hidden {
		attrs |= windows.FILE_ATTRIBUTE_HIDDEN
	} else {
		attrs &^= windows.FILE_ATTRIBUTE_HIDDEN
	}
	if err := windows.SetFileAttributes(ptr, attrs); err != nil {
		return fmt.Errorf("设置隐藏属性失败: %w", err)
	}
	return nil
}

// listFolderEntries 枚举父目录下的子目录，包括名字以句点结尾的隐藏目录。
func listFolderEntries(parent string) ([]FolderEntry, error) {
	clean, err := canonicalPath(parent)
	if err != nil {
		return nil, err
	}
	if !isDirectoryRaw(clean) {
		return nil, fmt.Errorf("父文件夹不存在: %s", clean)
	}

	ep, err := extendedPath(clean)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(ep)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	result := make([]FolderEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		full := joinChildPath(clean, name)
		if !isDirectoryRaw(full) {
			continue
		}
		status := "visible"
		if isHiddenName(name) {
			status = "hidden"
		}
		result = append(result, FolderEntry{Name: name, Path: full, Status: status})
	}
	return result, nil
}

// hideFolder 将目录改名为带 .. 后缀的形式，并加上隐藏属性。
func hideFolder(path string) (string, error) {
	clean, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	if !isDirectoryRaw(clean) {
		return "", fmt.Errorf("目标不是有效文件夹: %s", clean)
	}
	name := baseName(clean)
	if isHiddenName(name) {
		return "", fmt.Errorf("文件夹已经处于隐藏状态")
	}

	target := clean + HideSuffix
	if pathExists(target) {
		return "", fmt.Errorf("隐藏后的名字已存在: %s", baseName(target))
	}

	if err := renamePath(clean, target); err != nil {
		return "", err
	}
	if err := setFolderHidden(target, true); err != nil {
		_ = renamePath(target, clean)
		return "", err
	}
	return target, nil
}

// renameThenHide 可选先把文件夹改为新名称，再执行隐藏。
func renameThenHide(path, newName string) (string, error) {
	clean, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	if !isDirectoryRaw(clean) {
		return "", fmt.Errorf("目标不是有效文件夹: %s", clean)
	}
	if isHiddenName(baseName(clean)) {
		return "", fmt.Errorf("文件夹已经处于隐藏状态")
	}

	working := clean
	renamed := false
	if strings.TrimSpace(newName) != "" {
		name := strings.TrimSpace(newName)
		if err := validateNewName(name); err != nil {
			return "", err
		}
		parent := filepath.Dir(clean)
		target := joinChildPath(parent, name)
		if pathExists(target) {
			return "", fmt.Errorf("新名称已被占用: %s", name)
		}
		if err := renamePath(clean, target); err != nil {
			return "", err
		}
		working = target
		renamed = true
	}

	hiddenPath, err := hideFolder(working)
	if err != nil {
		if renamed {
			_ = renamePath(working, clean)
		}
		return "", err
	}
	return hiddenPath, nil
}

// showFolder 去掉 .. 后缀并清除隐藏属性，恢复原目录名。
func showFolder(path string) (string, error) {
	clean, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	name := baseName(clean)
	if !isHiddenName(name) {
		return "", fmt.Errorf("该文件夹不是方案 A 的隐藏状态")
	}

	target := originalNameFromHidden(clean)
	if pathExists(target) {
		return "", fmt.Errorf("原名已被占用: %s", baseName(target))
	}
	if err := setFolderHidden(clean, false); err != nil {
		return "", err
	}
	if err := renamePath(clean, target); err != nil {
		_ = setFolderHidden(clean, true)
		return "", err
	}
	return target, nil
}
