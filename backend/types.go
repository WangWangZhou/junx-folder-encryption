package backend

import (
	"fmt"
	"path/filepath"
	"strings"
)

// HideSuffix 是方案 A 使用的隐藏后缀。
// Windows 的 Win32 路径解析会去掉路径末尾的句点，因此带有该后缀的文件夹
// 在资源管理器中双击时会被解析成一个不存在的名字，从而无法打开。
const HideSuffix = ".."

// JunxProgress 是加密/解密进度事件载荷。
// Phase 使用固定代码（scan/build/encrypt/write/verify/extract/done），由前端翻译。
type JunxProgress struct {
	TaskID  string // 批量任务 ID；单任务调用为空，前端据此区分并发进度流
	Percent int
	Phase   string
	Current string
}

// DropFiles 是拖放事件载荷。Target 为带 data-file-drop-target 的元素 id。
type DropFiles struct {
	Paths  []string
	Target string
}

// FolderEntry 表示父文件夹中的一个子目录。
type FolderEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"` // visible 或 hidden
}

// isHiddenName 判断一个目录名是否处于方案 A 的隐藏状态。
// 单独的名字 ".." 或 "." 是相对路径标记，不能视为隐藏目录。
func isHiddenName(name string) bool {
	return len(name) > len(HideSuffix) && strings.HasSuffix(name, HideSuffix)
}

// originalNameFromHidden 从隐藏名还原原始名。
func originalNameFromHidden(name string) string {
	if !isHiddenName(name) {
		return name
	}
	return strings.TrimSuffix(name, HideSuffix)
}

// validateNewName 校验用户填写的新文件夹名称。
func validateNewName(name string) error {
	if name == "" {
		return fmt.Errorf("新文件夹名称不能为空")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("新文件夹名称不能以空格开头或结尾")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("新文件夹名称不能为 . 或 ..")
	}
	if strings.ContainsAny(name, `\/:*?"<>|`) {
		return fmt.Errorf("新文件夹名称包含 Windows 不允许的字符")
	}
	if strings.HasSuffix(name, HideSuffix) {
		return fmt.Errorf("新文件夹名称不能以 %s 结尾", HideSuffix)
	}
	return nil
}

// canonicalPath 返回不带 \\?\ 前缀的绝对路径，且完整保留最后一段的名字。
// 关键点：不能对整个路径调用 filepath.Clean，否则 "foo.." 会被规范化成 "foo"。
func canonicalPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("路径为空")
	}
	p = strings.TrimPrefix(p, `\\?\`)

	dir, base := filepath.Split(p)
	if base == "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		return strings.ReplaceAll(abs, "/", `\`), nil
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absDir = strings.ReplaceAll(absDir, "/", `\`)
	if !strings.HasSuffix(absDir, `\`) {
		absDir += `\`
	}
	return absDir + base, nil
}

// joinChildPath 拼接路径时不调用 Clean，避免破坏子目录名末尾的句点。
func joinChildPath(dir, name string) string {
	if strings.HasSuffix(dir, `\`) {
		return dir + name
	}
	return dir + `\` + name
}

// baseName 返回路径最后一段，保留末尾句点。
func baseName(p string) string {
	_, base := filepath.Split(p)
	return base
}
