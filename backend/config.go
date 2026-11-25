package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/hujson"
)

const (
	configFileName       = "config.jsonc"
	configDirName        = "FolderHideTool"
	legacyPinFileName    = "pin.json"
	configFormatVersion  = 1
	legacyConfigMigrated = true
)

// AppConfig 是 config.jsonc 的完整内容。
// 明文 PIN 不会写入此结构，pin 中只保存加密信封与状态字段。
type AppConfig struct {
	Version           int        `json:"version"`
	Language          string     `json:"language"`
	AlwaysOnTop       bool       `json:"alwaysOnTop"`
	BatchFile         bool       `json:"batchFile"`
	BatchFolder       bool       `json:"batchFolder"`
	LogLevel          string     `json:"logLevel"`
	LegacyMigrated    bool       `json:"legacyMigrated"`
	LegacyPinMigrated bool       `json:"legacyPinMigrated"`
	Pin               *pinRecord `json:"pin"`
}

// ConfigInfo 是提供给前端展示的配置摘要，不包含 PIN 密文。
type ConfigInfo struct {
	Language       string `json:"language"`
	AlwaysOnTop    bool   `json:"alwaysOnTop"`
	BatchFile      bool   `json:"batchFile"`
	BatchFolder    bool   `json:"batchFolder"`
	LogLevel       string `json:"logLevel"`
	ConfigPath     string `json:"configPath"`
	LogDir         string `json:"logDir"`
	LogFilePath    string `json:"logFilePath"`
	NeedsMigration bool   `json:"needsMigration"`
}

type configStore struct {
	path string
}

func newConfigStore(path string) *configStore {
	return &configStore{path: path}
}

func defaultConfigStore() (*configStore, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	store := newConfigStore(path)
	if err := store.ensure(); err != nil {
		return nil, err
	}
	if err := migrateLegacyPin(store); err != nil {
		return nil, err
	}
	return store, nil
}

func resolveConfigPath() (string, error) {
	if path, err := readConfigPath(); err == nil && path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("获取配置目录失败: %w", err)
	}
	return filepath.Join(dir, configDirName, configFileName), nil
}

func (cs *configStore) ensure() error {
	if _, err := os.Stat(cs.path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	defaults := defaultAppConfig()
	return cs.writeTo(cs.path, defaults)
}

func (cs *configStore) Load() (AppConfig, error) {
	data, err := os.ReadFile(cs.path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := defaultAppConfig()
		if saveErr := cs.writeTo(cs.path, cfg); saveErr != nil {
			return cfg, saveErr
		}
		return cfg, nil
	}
	if err != nil {
		return AppConfig{}, err
	}
	standard, err := hujson.Standardize(data)
	if err != nil {
		return AppConfig{}, fmt.Errorf("配置文件格式错误: %w", err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(standard, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("配置文件解析失败: %w", err)
	}
	normalizeAppConfig(&cfg)
	return cfg, nil
}

func (cs *configStore) Save(cfg AppConfig) error {
	normalizeAppConfig(&cfg)
	data, err := os.ReadFile(cs.path)
	if errors.Is(err, os.ErrNotExist) {
		return cs.writeTo(cs.path, cfg)
	}
	if err != nil {
		return err
	}
	root, err := hujson.Parse(data)
	if err != nil {
		return fmt.Errorf("配置文件格式错误: %w", err)
	}
	if err := patchAppConfig(&root, cfg); err != nil {
		return err
	}
	return writeFileAtomic(cs.path, root.Pack(), 0o600)
}

func (cs *configStore) writeTo(path string, cfg AppConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(configTemplate(cfg)), 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		Version:           configFormatVersion,
		Language:          "zh-CN",
		AlwaysOnTop:       true,
		BatchFile:         true,
		BatchFolder:       true,
		LogLevel:          "normal",
		LegacyMigrated:    false,
		LegacyPinMigrated: false,
		Pin:               nil,
	}
}

func normalizeAppConfig(cfg *AppConfig) {
	if cfg.Version != configFormatVersion {
		cfg.Version = configFormatVersion
	}
	cfg.Language = normalizeAppLanguage(cfg.Language)
	cfg.LogLevel = normalizeLogLevel(cfg.LogLevel)
	if cfg.Pin != nil {
		cfg.Pin.Magic = pinMagic
		cfg.Pin.Version = pinVersion
		cfg.Pin.Policy = normalizePinPolicy(cfg.Pin.Policy)
	}
}

func normalizeAppLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "en":
		return "en"
	case "ja":
		return "ja"
	default:
		return "zh-CN"
	}
}

func normalizeLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "off":
		return "off"
	case "trace":
		return "trace"
	case "debug":
		return "debug"
	default:
		return "normal"
	}
}

func patchAppConfig(root *hujson.Value, cfg AppConfig) error {
	values := map[string]string{
		"/version":           strconv.Itoa(cfg.Version),
		"/language":          strconv.Quote(cfg.Language),
		"/alwaysOnTop":       strconv.FormatBool(cfg.AlwaysOnTop),
		"/batchFile":         strconv.FormatBool(cfg.BatchFile),
		"/batchFolder":       strconv.FormatBool(cfg.BatchFolder),
		"/logLevel":          strconv.Quote(cfg.LogLevel),
		"/legacyMigrated":    strconv.FormatBool(cfg.LegacyMigrated),
		"/legacyPinMigrated": strconv.FormatBool(cfg.LegacyPinMigrated),
	}
	for ptr, raw := range values {
		if err := patchJSONValue(root, ptr, raw); err != nil {
			return err
		}
	}
	pinRaw := "null"
	if cfg.Pin != nil {
		pinRaw = pinJSONC(*cfg.Pin)
	}
	return patchJSONValue(root, "/pin", pinRaw)
}

func patchJSONValue(root *hujson.Value, ptr, raw string) error {
	parsed, err := hujson.Parse([]byte(raw))
	if err != nil {
		return fmt.Errorf("生成配置值失败: %w", err)
	}
	target := root.Find(ptr)
	if target == nil {
		// 兼容旧版本配置：缺少字段时补写，避免新增配置项导致保存失败。
		patch := []byte(`[{"op":"add","path":"` + ptr + `","value":` + raw + `}]`)
		if err := root.Patch(patch); err != nil {
			return fmt.Errorf("补充配置字段 %s 失败: %w", ptr, err)
		}
		root.Format()
		return nil
	}
	target.Value = parsed.Value
	return nil
}

func configTemplate(cfg AppConfig) string {
	pin := "null"
	if cfg.Pin != nil {
		pin = pinJSONC(*cfg.Pin)
	}
	return fmt.Sprintf(`{
  // 配置文件格式版本，请勿手动修改
  "version": %d,

  // 显示语言：zh-CN / en / ja
  "language": %s,

  // 窗口是否总在最前（播放器式置顶）
  "alwaysOnTop": %s,

  // 是否启用加密批量任务（兼容旧版字段，与 batchFolder 同步）
  "batchFile": %s,

  // 是否启用加密批量任务（兼容旧版字段，与 batchFile 同步）
  "batchFolder": %s,

  // 文件日志级别：off / trace / debug / normal
  "logLevel": %s,

  // 是否已从旧版 localStorage 迁移设置，请勿手动修改
  "legacyMigrated": %s,

  // 是否已从旧版 pin.json 迁移锁屏密码，请勿手动修改
  "legacyPinMigrated": %s,

  // 锁屏密码；null 表示未设置。明文密码不会写入此文件
  "pin": %s
}
`,
		cfg.Version,
		strconv.Quote(cfg.Language),
		strconv.FormatBool(cfg.AlwaysOnTop),
		strconv.FormatBool(cfg.BatchFile),
		strconv.FormatBool(cfg.BatchFolder),
		strconv.Quote(cfg.LogLevel),
		strconv.FormatBool(cfg.LegacyMigrated),
		strconv.FormatBool(cfg.LegacyPinMigrated),
		pin,
	)
}

func pinJSONC(record pinRecord) string {
	return fmt.Sprintf(`{
    // 配置标记，请勿手动修改
    "magic": %s,
    // PIN 记录版本，请勿手动修改
    "version": %d,
    // AES-GCM nonce，base64 编码
    "nonce": %s,
    // 加密后的 PIN 信封，base64 编码；不保存明文
    "ciphertext": %s,
    // PIN 位数，当前固定为 4
    "length": %d,
    // 密码策略，当前固定 unlimited
    "policy": %s,
    // 密码错误次数
    "failedCount": %d,
    // 错误计数窗口开始时间，Unix 毫秒
    "windowStart": %d,
    // 临时锁定截止时间，Unix 毫秒
    "lockUntil": %d,
    // 是否开启 15 分钟空闲自动锁定
    "autoLock": %s
  }`,
		strconv.Quote(record.Magic),
		record.Version,
		strconv.Quote(record.Nonce),
		strconv.Quote(record.Ciphertext),
		record.Length,
		strconv.Quote(record.Policy),
		record.FailedCount,
		record.WindowStart,
		record.LockUntil,
		strconv.FormatBool(record.AutoLock),
	)
}

func migrateLegacyPin(cs *configStore) error {
	cfg, err := cs.Load()
	if err != nil {
		return err
	}
	// 迁移只做一次：用户手动把 pin 改成 null 后，旧 pin.json 不再重新写回。
	if cfg.LegacyPinMigrated {
		return nil
	}
	if cfg.Pin != nil {
		cfg.LegacyPinMigrated = true
		return cs.Save(cfg)
	}

	legacy := filepath.Join(filepath.Dir(cs.path), legacyPinFileName)
	data, err := os.ReadFile(legacy)
	if errors.Is(err, os.ErrNotExist) {
		cfg.LegacyPinMigrated = true
		return cs.Save(cfg)
	}
	if err != nil {
		return err
	}
	var record pinRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("旧密码配置解析失败: %w", err)
	}
	if record.Magic != pinMagic || record.Version != pinVersion || !validPinLength(record.Length) {
		return fmt.Errorf("旧密码配置版本不受支持")
	}
	normalizeAppConfig(&cfg)
	cfg.Pin = &record
	cfg.LegacyPinMigrated = true
	if err := cs.Save(cfg); err != nil {
		return err
	}
	if err := os.Rename(legacy, legacy+".migrated"); err != nil && !errors.Is(err, os.ErrNotExist) {
		// 配置已写入成功，改名失败只保留旧文件作为备份，不影响启动。
		return nil
	}
	return nil
}

func defaultLogDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), configDirName, "logs")
	}
	return filepath.Join(dir, configDirName, "logs")
}

func currentLogPath() string {
	return filepath.Join(defaultLogDir(), "app-"+time.Now().Format("2006-01-02")+".log")
}
