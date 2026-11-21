package backend

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	pinMagic     = "FOLDERHIDE-PIN"
	pinVersion   = 1
	pinPrefix    = "snipaste"
	pinPolicyWin = "window"
	pinPolicyInf = "unlimited"

	pinWindow       = 5 * time.Minute
	pinMaxFailures  = 10
	pinAutoLockIdle = 15 * time.Minute
	pinFileName     = "pin.json"
	pinFileDirName  = "FolderHideTool"
)

// autoLockDialogText 是"自动锁定"原生对话框的三语文案。
type autoLockDialogText struct {
	Title       string
	Question    string
	Yes         string
	No          string
	InfoTitle   string
	InfoMessage string
}

func normalizedLocale(locale string) string {
	value := strings.ToLower(locale)
	if strings.HasPrefix(value, "ja") {
		return "ja"
	}
	if strings.HasPrefix(value, "en") {
		return "en"
	}
	return "zh-CN"
}

func autoLockDialogTextFor(locale string) autoLockDialogText {
	switch normalizedLocale(locale) {
	case "en":
		return autoLockDialogText{
			Title:       "Auto-lock",
			Question:    "Lock immediately?",
			Yes:         "Yes",
			No:          "No",
			InfoTitle:   "Auto-lock",
			InfoMessage: "Auto-lock in 15 minutes.",
		}
	case "ja":
		return autoLockDialogText{
			Title:       "自動ロック",
			Question:    "今すぐロックしますか？",
			Yes:         "はい",
			No:          "いいえ",
			InfoTitle:   "自動ロック",
			InfoMessage: "15分後に自動ロックします。",
		}
	default:
		return autoLockDialogText{
			Title:       "自动锁定",
			Question:    "是否立即锁定？",
			Yes:         "是",
			No:          "否",
			InfoTitle:   "自动锁定",
			InfoMessage: "15分钟后自动锁定。",
		}
	}
}

// pinDeletedNotice 是关闭打开密码后原生提示框的三语文案。
type pinDeletedNotice struct {
	Title   string
	Message string
}

func pinDeletedNoticeFor(locale string) pinDeletedNotice {
	switch normalizedLocale(locale) {
	case "en":
		return pinDeletedNotice{Title: "Opening password", Message: "The lock password has been deleted."}
	case "ja":
		return pinDeletedNotice{Title: "起動パスワード", Message: "ロックパスワードが削除されました。"}
	default:
		return pinDeletedNotice{Title: "打开密码", Message: "锁定密码已删除。"}
	}
}

// PinState 是前端展示用的密码状态，不包含任何密文或明文密码。
type PinState struct {
	Enabled          bool
	Length           int
	Policy           string
	Locked           bool
	RemainingSeconds int
	AttemptsLeft     int  // -1 表示无限次
	AutoLock         bool // 是否开启空闲 15 分钟自动锁定
	SessionLocked    bool // 当前会话是否已锁定（需输入密码解锁）
}

// PinResult 是 PIN 操作的结果码与最新状态。
type PinResult struct {
	OK    bool
	Code  string
	State PinState
}

// pinRecord 是持久化到 pin.json 的内容。
// Nonce/Ciphertext 是 AES("snipaste"+PIN) 的 GCM 信封，不保存明文 PIN。
type pinRecord struct {
	Magic       string `json:"magic"`
	Version     int    `json:"version"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
	Length      int    `json:"length"`
	Policy      string `json:"policy"`
	FailedCount int    `json:"failedCount"`
	WindowStart int64  `json:"windowStart"` // UnixMilli
	LockUntil   int64  `json:"lockUntil"`   // UnixMilli
	AutoLock    bool   `json:"autoLock"`
}

type pinStore struct {
	path string
}

func newPinStore(path string) *pinStore {
	return &pinStore{path: path}
}

func defaultPinStore() (*pinStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("获取配置目录失败: %w", err)
	}
	return newPinStore(filepath.Join(dir, pinFileDirName, pinFileName)), nil
}

func (ps *pinStore) Load() (pinRecord, bool, error) {
	data, err := os.ReadFile(ps.path)
	if errors.Is(err, os.ErrNotExist) {
		return pinRecord{}, false, nil
	}
	if err != nil {
		return pinRecord{}, false, err
	}
	var record pinRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return pinRecord{}, false, fmt.Errorf("密码配置文件格式损坏: %w", err)
	}
	if record.Magic != pinMagic || record.Version != pinVersion {
		return pinRecord{}, false, fmt.Errorf("密码配置文件版本不受支持")
	}
	if !validPinLength(record.Length) {
		return pinRecord{}, false, fmt.Errorf("密码长度不受支持: %d", record.Length)
	}
	record.Policy = normalizePinPolicy(record.Policy)
	return record, true, nil
}

func (ps *pinStore) Save(record pinRecord) error {
	record.Magic = pinMagic
	record.Version = pinVersion
	record.Policy = normalizePinPolicy(record.Policy)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ps.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(ps.path, data, 0o600)
}

func (ps *pinStore) Clear() error {
	if err := os.Remove(ps.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validPinLength(length int) bool {
	return length == 3 || length == 4 || length == 6
}

func normalizePinPolicy(policy string) string {
	if policy == pinPolicyInf {
		return pinPolicyInf
	}
	return pinPolicyWin
}

func validPin(pin string, length int) bool {
	if !validPinLength(length) || len(pin) != length {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sealPIN 用内置 AES key 加密 "snipaste"+PIN，返回 base64 的 nonce 与密文。
func sealPIN(pin string) (string, string, error) {
	block, err := aes.NewCipher(metadataKey())
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(pinPrefix+pin), nil)
	return base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(ciphertext), nil
}

// openPIN 解密信封并用常量时间比较校验 PIN。
func openPIN(record pinRecord, pin string) (bool, error) {
	nonce, err := base64.StdEncoding.DecodeString(record.Nonce)
	if err != nil {
		return false, fmt.Errorf("密码 nonce 无效: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(record.Ciphertext)
	if err != nil {
		return false, fmt.Errorf("密码密文无效: %w", err)
	}
	block, err := aes.NewCipher(metadataKey())
	if err != nil {
		return false, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return false, err
	}
	if len(nonce) != gcm.NonceSize() {
		return false, fmt.Errorf("密码 nonce 长度错误")
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return false, fmt.Errorf("密码解密失败: %w", err)
	}
	expected := []byte(pinPrefix + pin)
	return subtle.ConstantTimeCompare(plain, expected) == 1, nil
}

func pinStateFromRecord(record pinRecord, configured bool, now time.Time) PinState {
	state := PinState{
		Enabled:      configured,
		Length:       record.Length,
		Policy:       record.Policy,
		AttemptsLeft: -1,
		AutoLock:     record.AutoLock,
	}
	if !configured {
		return state
	}

	nowMs := now.UnixMilli()
	if record.LockUntil > nowMs {
		state.Locked = true
		state.RemainingSeconds = int((record.LockUntil - nowMs + 999) / 1000)
	}

	if state.Policy == pinPolicyWin {
		if state.Locked {
			state.AttemptsLeft = 0
		} else {
			count := record.FailedCount
			if record.WindowStart == 0 || nowMs-record.WindowStart >= pinWindow.Milliseconds() {
				count = 0
			}
			left := pinMaxFailures - count
			if left < 0 {
				left = 0
			}
			if left > pinMaxFailures {
				left = pinMaxFailures
			}
			state.AttemptsLeft = left
		}
	}
	return state
}

func recordPinFailure(record *pinRecord, now time.Time) {
	if record.Policy == pinPolicyInf {
		record.FailedCount++
		return
	}
	nowMs := now.UnixMilli()
	if record.WindowStart == 0 || nowMs-record.WindowStart >= pinWindow.Milliseconds() {
		record.FailedCount = 0
		record.WindowStart = nowMs
	}
	record.FailedCount++
	if record.FailedCount >= pinMaxFailures {
		record.FailedCount = pinMaxFailures
		record.WindowStart = nowMs
		record.LockUntil = now.Add(pinWindow).UnixMilli()
	}
}

func resetPinFailures(record *pinRecord) {
	record.FailedCount = 0
	record.WindowStart = 0
	record.LockUntil = 0
}

func newPinResult(code string, state PinState) PinResult {
	return PinResult{
		OK:    code == "ok" || code == "disabled",
		Code:  code,
		State: state,
	}
}
