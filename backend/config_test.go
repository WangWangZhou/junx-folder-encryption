package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigJSONCKeepsComments(t *testing.T) {
	store := newConfigStore(filepath.Join(t.TempDir(), configFileName))
	cfg := defaultAppConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "// 显示语言") || !strings.Contains(content, "// 文件日志级别") || !strings.Contains(content, "// 锁屏密码") {
		t.Fatalf("配置模板缺少注释: %s", content)
	}

	cfg.LogLevel = "trace"
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	content = string(data)
	if !strings.Contains(content, `"logLevel": "trace"`) {
		t.Fatalf("日志级别未写入: %s", content)
	}
	if !strings.Contains(content, "// 显示语言") {
		t.Fatalf("回写后注释丢失: %s", content)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.LogLevel != "trace" {
		t.Fatalf("日志级别回读错误: %s", got.LogLevel)
	}
}

func TestMigrateLegacyPinIntoConfig(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, legacyPinFileName)
	nonce, ciphertext, err := sealPIN("1234")
	if err != nil {
		t.Fatal(err)
	}
	record := pinRecord{
		Magic:      pinMagic,
		Version:    pinVersion,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Length:     4,
		Policy:     pinPolicyInf,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store := newConfigStore(filepath.Join(dir, configFileName))
	if err := store.ensure(); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyPin(store); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pin == nil || cfg.Pin.Length != 4 {
		t.Fatalf("旧 PIN 未迁移: %+v", cfg.Pin)
	}
	if !cfg.LegacyPinMigrated {
		t.Fatal("旧 PIN 迁移标记未写入")
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Fatalf("旧文件未备份: %v", err)
	}
}

func TestMigrateLegacyPinOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, legacyPinFileName)
	nonce, ciphertext, err := sealPIN("1234")
	if err != nil {
		t.Fatal(err)
	}
	record := pinRecord{
		Magic:      pinMagic,
		Version:    pinVersion,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Length:     4,
		Policy:     pinPolicyInf,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store := newConfigStore(filepath.Join(dir, configFileName))
	if err := store.ensure(); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyPin(store); err != nil {
		t.Fatal(err)
	}

	// 模拟用户手动清空锁屏密码，并让旧 pin.json 再次出现。
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Pin = nil
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyPin(store); err != nil {
		t.Fatal(err)
	}
	cfg, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pin != nil {
		t.Fatalf("手动清空后旧 PIN 被重新写回: %+v", cfg.Pin)
	}
}

func TestResetConfigClearsPinAndState(t *testing.T) {
	store := newConfigStore(filepath.Join(t.TempDir(), configFileName))
	if err := store.ensure(); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	nonce, ciphertext, err := sealPIN("1234")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Pin = &pinRecord{
		Magic:      pinMagic,
		Version:    pinVersion,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Length:     4,
		Policy:     pinPolicyInf,
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	service := &FolderService{
		config:   store,
		pins:     &pinStore{config: store},
		logs:     []string{},
		unlocked: false,
		autoLock: true,
	}
	if err := service.ResetConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Pin != nil {
		t.Fatalf("恢复默认后 PIN 未清除: %+v", got.Pin)
	}
	if got.LogLevel != "normal" || got.AlwaysOnTop != true || !got.BatchFile || !got.BatchFolder {
		t.Fatalf("恢复默认值错误: %+v", got)
	}
	if !got.LegacyMigrated || !got.LegacyPinMigrated {
		t.Fatal("恢复默认后迁移标记应为 true")
	}
	service.mu.Lock()
	unlocked := service.unlocked
	autoLock := service.autoLock
	service.mu.Unlock()
	if !unlocked || autoLock {
		t.Fatalf("恢复默认后会话状态错误: unlocked=%v autoLock=%v", unlocked, autoLock)
	}
}

func TestNormalizeLogLevel(t *testing.T) {
	cases := map[string]string{
		"OFF":     "off",
		"Trace":   "trace",
		" debug ": "debug",
		"other":   "normal",
	}
	for input, want := range cases {
		if got := normalizeLogLevel(input); got != want {
			t.Fatalf("normalizeLogLevel(%q) = %q, want %q", input, got, want)
		}
	}
}
