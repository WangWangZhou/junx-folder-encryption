package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPinValidationAndSeal(t *testing.T) {
	if !validPin("123", 3) || !validPin("0007", 4) || !validPin("123456", 6) {
		t.Fatal("合法 PIN 应通过校验")
	}
	if validPin("12", 3) || validPin("1234", 3) || validPin("12a", 3) || validPin("１２３", 3) {
		t.Fatal("非法 PIN 应被拒绝")
	}

	nonce, ciphertext, err := sealPIN("007")
	if err != nil {
		t.Fatal(err)
	}
	record := pinRecord{Nonce: nonce, Ciphertext: ciphertext, Length: 3, Policy: pinPolicyWin}
	ok, err := openPIN(record, "007")
	if err != nil || !ok {
		t.Fatalf("正确 PIN 应校验通过: ok=%v err=%v", ok, err)
	}
	ok, err = openPIN(record, "008")
	if err != nil || ok {
		t.Fatalf("错误 PIN 应校验失败: ok=%v err=%v", ok, err)
	}
}

func TestPinWindowLockoutAndExpiry(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}

	if result := service.ConfigurePin("1234", 4, pinPolicyWin); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}
	service.mu.Lock()
	service.unlocked = false
	service.mu.Unlock()

	for i := 0; i < pinMaxFailures; i++ {
		result := service.Unlock("0000")
		if result.OK {
			t.Fatal("错误 PIN 不应解锁")
		}
	}
	state := service.GetPinState()
	if !state.Locked || state.AttemptsLeft != 0 {
		t.Fatalf("10 次失败后应锁定: %+v", state)
	}
	if result := service.Unlock("1234"); result.Code != "locked" {
		t.Fatalf("锁定期间正确 PIN 也应拒绝, got %s", result.Code)
	}

	record, configured, err := store.Load()
	if err != nil || !configured {
		t.Fatal(err)
	}
	record.LockUntil = time.Now().Add(-time.Second).UnixMilli()
	record.WindowStart = 0
	record.FailedCount = 0
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	if result := service.Unlock("1234"); !result.OK {
		t.Fatalf("锁定到期后正确 PIN 应解锁: %+v", result)
	}
	if state := service.GetPinState(); state.Locked || state.AttemptsLeft != pinMaxFailures {
		t.Fatalf("解锁后计数应清零: %+v", state)
	}
}

func TestPinUnlimitedNeverLocks(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}

	if result := service.ConfigurePin("123456", 6, pinPolicyInf); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}
	service.mu.Lock()
	service.unlocked = false
	service.mu.Unlock()

	for i := 0; i < 20; i++ {
		service.Unlock("000000")
	}
	state := service.GetPinState()
	if state.Locked || state.AttemptsLeft != -1 {
		t.Fatalf("无限次策略不应锁定: %+v", state)
	}
	if result := service.Unlock("123456"); !result.OK {
		t.Fatalf("正确 PIN 应解锁: %+v", result)
	}
}

func TestPinRecordHasNoPlaintext(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}
	if result := service.ConfigurePin("001", 3, pinPolicyWin); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, pinPrefix+"001") || strings.Contains(content, `"password"`) {
		t.Fatalf("密码配置不应包含明文 PIN: %s", content)
	}
	if !strings.Contains(content, `"nonce"`) || !strings.Contains(content, `"ciphertext"`) {
		t.Fatalf("密码配置应包含加密信封: %s", content)
	}
}

func TestManualLockRequiresUnlockAgain(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}
	if result := service.ConfigurePin("1234", 4, pinPolicyInf); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}

	result := service.Lock()
	if !result.OK || !result.State.SessionLocked {
		t.Fatalf("手动锁定后会话应为锁定态: %+v", result)
	}
	if err := service.EncryptFolder(`C:\Windows`, ""); err == nil || !strings.Contains(err.Error(), "请先输入打开密码") {
		t.Fatalf("手动锁定后应拒绝加密: %v", err)
	}
	if result := service.Unlock("1234"); !result.OK || result.State.SessionLocked {
		t.Fatalf("重新解锁应成功并解除会话锁定: %+v", result)
	}
}

func TestAutoLockAfterIdleAndPersistence(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}
	if result := service.ConfigurePin("1234", 4, pinPolicyInf); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}
	if err := service.saveAutoLock(true); err != nil {
		t.Fatal(err)
	}
	if state := service.GetPinState(); !state.AutoLock || state.SessionLocked {
		t.Fatalf("自动锁定应开启且会话未锁定: %+v", state)
	}

	// 模拟解锁成功后已空闲 16 分钟。
	service.mu.Lock()
	service.unlockedAt = time.Now().Add(-16 * time.Minute)
	service.mu.Unlock()
	if state := service.GetPinState(); !state.SessionLocked {
		t.Fatalf("空闲超过 15 分钟后应自动锁定: %+v", state)
	}
	if err := service.EncryptFolder(`C:\Windows`, ""); err == nil || !strings.Contains(err.Error(), "请先输入打开密码") {
		t.Fatalf("自动锁定后应拒绝加密: %v", err)
	}
	if result := service.Unlock("1234"); !result.OK || result.State.SessionLocked {
		t.Fatalf("自动锁定后应可用密码解锁: %+v", result)
	}
	if state := service.GetPinState(); state.SessionLocked {
		t.Fatalf("解锁后应重置计时并解除锁定: %+v", state)
	}

	record, configured, err := store.Load()
	if err != nil || !configured || !record.AutoLock {
		t.Fatalf("自动锁定开关应持久化: configured=%v record=%+v err=%v", configured, record, err)
	}
}

func TestDisablePinClearsAutoLock(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}
	if result := service.ConfigurePin("1234", 4, pinPolicyInf); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}
	if err := service.saveAutoLock(true); err != nil {
		t.Fatal(err)
	}
	if result := service.DisablePin("zh-CN"); !result.OK {
		t.Fatalf("关闭密码失败: %+v", result)
	}
	if state := service.GetPinState(); state.Enabled || state.AutoLock || state.SessionLocked {
		t.Fatalf("关闭密码后应同时清除自动锁定: %+v", state)
	}
}

func TestServiceRequiresUnlock(t *testing.T) {
	store := newPinStore(filepath.Join(t.TempDir(), pinFileName))
	service := &FolderService{pins: store, logs: []string{}, unlocked: true}
	if result := service.ConfigurePin("123", 3, pinPolicyWin); !result.OK {
		t.Fatalf("设置 PIN 失败: %+v", result)
	}
	service.mu.Lock()
	service.unlocked = false
	service.mu.Unlock()

	err := service.EncryptFolder(`C:\Windows`, "")
	if err == nil || !strings.Contains(err.Error(), "请先输入打开密码") {
		t.Fatalf("未解锁时应拒绝加密: %v", err)
	}
	if result := service.Unlock("123"); !result.OK {
		t.Fatalf("解锁失败: %+v", result)
	}
	err = service.EncryptFolder(`C:\Windows`, "")
	if err == nil || !strings.Contains(err.Error(), "拒绝操作系统目录") {
		t.Fatalf("解锁后应进入正常保护路径检查: %v", err)
	}
}
