//go:build windows

package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestACLHideShowRoundTrip(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "隐私资料")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := hideWithACL(folder); err != nil {
		t.Fatalf("方案 D 加密失败: %v", err)
	}
	if !isDirectoryRaw(folder) {
		t.Fatal("加密后目录不应改名")
	}
	dsPath := joinChildPath(folder, dsStoreName)
	if !pathExists(dsPath) {
		t.Fatal("权限备份未写入 .DS_Store")
	}
	if _, err := os.ReadDir(folder); err == nil {
		t.Fatal("拒绝列目录后仍可枚举目录")
	}
	if _, err := os.ReadFile(dsPath); err != nil {
		t.Fatalf("工具应能读取内部 .DS_Store: %v", err)
	}

	warning, err := showWithACL(folder)
	if err != nil {
		t.Fatalf("方案 D 解密失败: %v", err)
	}
	if warning != "" {
		t.Fatalf("解密时出现意外警告: %s", warning)
	}
	if _, err := os.ReadDir(folder); err != nil {
		t.Fatalf("恢复后无法枚举目录: %v", err)
	}
	if pathExists(dsPath) {
		t.Fatal("恢复后 .DS_Store 未删除")
	}
}

func TestACLHideRefusesExistingMetadata(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "隐私资料")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, dsStoreName), []byte("real mac metadata"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := hideWithACL(folder); err == nil {
		t.Fatal("已存在 .DS_Store 时应当拒绝覆盖")
	}
	if _, err := os.ReadDir(folder); err != nil {
		t.Fatalf("失败后目录权限不应被改变: %v", err)
	}
}

func TestACLBrokenMetadataDoesNotRestore(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "隐私资料")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := hideWithACL(folder); err != nil {
		t.Fatal(err)
	}

	data, err := readDSStore(folder)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decryptPayload(data)
	if err != nil {
		t.Fatal(err)
	}
	var backup aclBackup
	if err := json.Unmarshal(payload, &backup); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = restoreACL(folder, backup)
		_ = removeDSStore(folder)
	}()

	if err := restoreACL(folder, backup); err != nil {
		t.Fatal(err)
	}
	if err := removeDSStore(folder); err != nil {
		t.Fatal(err)
	}
	if err := writeDSStore(folder, []byte("bad metadata")); err != nil {
		t.Fatal(err)
	}
	if err := applyListDeny(folder); err != nil {
		t.Fatal(err)
	}
	if _, err := showWithACL(folder); err == nil {
		t.Fatal("损坏元数据不应允许恢复")
	}
	if _, err := os.ReadDir(folder); err == nil {
		t.Fatal("恢复失败后目录仍应保持拒绝列目录")
	}
}
