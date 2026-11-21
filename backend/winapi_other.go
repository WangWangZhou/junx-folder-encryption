//go:build !windows

package backend

import "fmt"

var errWindowsOnly = fmt.Errorf("文件夹隐藏工具仅支持 Windows")

func listFolderEntries(parent string) ([]FolderEntry, error) {
	return nil, errWindowsOnly
}

func hideFolder(path string) (string, error) {
	return "", errWindowsOnly
}

func renameThenHide(path, newName string) (string, error) {
	return "", errWindowsOnly
}

func showFolder(path string) (string, error) {
	return "", errWindowsOnly
}
