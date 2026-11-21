//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHideShowRoundTrip(t *testing.T) {
	root, err := os.MkdirTemp("", "folderhide-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	original := filepath.Join(root, "测试目录")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}

	hiddenPath, err := hideFolder(original)
	if err != nil {
		t.Fatalf("隐藏失败: %v", err)
	}
	if !strings.HasSuffix(hiddenPath, HideSuffix) {
		t.Fatalf("隐藏路径后缀错误: %s", hiddenPath)
	}
	if !pathExists(hiddenPath) {
		t.Fatalf("隐藏目录不存在: %s", hiddenPath)
	}
	if pathExists(original) {
		t.Fatal("隐藏后原名仍可访问")
	}

	entries, err := listFolderEntries(root)
	if err != nil {
		t.Fatalf("枚举失败: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("期望 1 个目录，实际 %d 个", len(entries))
	}
	if entries[0].Status != "hidden" {
		t.Fatalf("状态识别错误: %s", entries[0].Status)
	}
	if entries[0].Name != "测试目录.." {
		t.Fatalf("隐藏名错误: %s", entries[0].Name)
	}

	if _, err := hideFolder(hiddenPath); err == nil {
		t.Fatal("重复隐藏应当失败")
	}

	restored, err := showFolder(hiddenPath)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if restored != original {
		t.Fatalf("恢复路径错误: %s", restored)
	}
	if !pathExists(original) {
		t.Fatal("恢复后原目录不存在")
	}
	if pathExists(hiddenPath) {
		t.Fatal("恢复后隐藏名仍可访问")
	}
}

func TestHideNameCollision(t *testing.T) {
	root, err := os.MkdirTemp("", "folderhide-collision-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	original := filepath.Join(root, "目标")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	ep, err := extendedPath(original + HideSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ep, 0o755); err != nil {
		t.Fatalf("创建冲突目录失败: %v", err)
	}
	defer os.RemoveAll(ep)

	if _, err := hideFolder(original); err == nil {
		t.Fatal("目标名冲突时应当失败")
	}
}

func TestRenameThenHide(t *testing.T) {
	root, err := os.MkdirTemp("", "folderhide-rename-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	original := filepath.Join(root, "旧名称")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}

	want := joinChildPath(root, "新名称"+HideSuffix)
	hidden, err := renameThenHide(original, "新名称")
	if err != nil {
		t.Fatalf("改名后隐藏失败: %v", err)
	}
	if hidden != want {
		t.Fatalf("隐藏路径错误: %s", hidden)
	}
	if pathExists(original) {
		t.Fatal("改名后原路径仍存在")
	}
	if !pathExists(hidden) {
		t.Fatal("新名称的隐藏目录不存在")
	}

	restored, err := showFolder(hidden)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if restored != joinChildPath(root, "新名称") {
		t.Fatalf("恢复路径错误: %s", restored)
	}
}

func TestRenameThenHideInvalidName(t *testing.T) {
	root, err := os.MkdirTemp("", "folderhide-invalid-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	original := filepath.Join(root, "原名称")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := renameThenHide(original, "a/b"); err == nil {
		t.Fatal("非法名称应当失败")
	}
	if !pathExists(original) {
		t.Fatal("失败后原目录应当保留")
	}
}
