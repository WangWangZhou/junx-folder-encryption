package backend

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// FolderService 暴露给前端的文件夹隐藏服务。
type FolderService struct {
	mu         sync.Mutex
	configMu   sync.Mutex
	logs       []string
	pins       *pinStore
	config     *configStore
	fileLog    *FileLogger
	unlocked   bool
	autoLock   bool      // 空闲自动锁定开关，与 config.jsonc 同步
	unlockedAt time.Time // 本次会话解锁成功的时间，用于 15 分钟计时
}

// NewFolderService 创建服务实例。
func NewFolderService() *FolderService {
	service := &FolderService{logs: []string{}}
	store, err := defaultPinStore()
	if err != nil {
		// 配置目录不可用时按未设置密码处理，避免启动失败。
		path := filepath.Join(os.TempDir(), configDirName, configFileName)
		store = newPinStore(path)
		service.pins = store
		service.unlocked = true
		service.config = store.config
		service.fileLog = NewFileLogger(defaultLogDir(), "normal")
		return service
	}
	service.pins = store
	service.config = store.config
	cfg, cfgErr := store.config.Load()
	if cfgErr == nil {
		service.fileLog = NewFileLogger(defaultLogDir(), cfg.LogLevel)
	} else {
		service.fileLog = NewFileLogger(defaultLogDir(), "normal")
	}
	record, configured, loadErr := store.Load()
	service.unlocked = loadErr != nil || !configured
	if loadErr == nil && configured {
		service.autoLock = record.AutoLock
	}
	service.unlockedAt = time.Now()
	return service
}

// ServiceName 返回服务名，供 Wails 日志与绑定生成使用。
func (s *FolderService) ServiceName() string {
	return "FolderService"
}

// Version 返回纯版本号，方案说明由前端按当前语言渲染。
func (s *FolderService) Version() string {
	return "1.1.0"
}

// Quit 退出应用程序。托盘菜单与常规页的退出按钮共用此入口。
func (s *FolderService) Quit() {
	if app := application.Get(); app != nil {
		app.Quit()
	}
}

// pinState 读取当前 PIN 状态；错误或缺失配置按未启用处理。
func (s *FolderService) pinState() PinState {
	s.mu.Lock()
	unlocked := s.unlocked
	autoLock := s.autoLock
	s.mu.Unlock()

	if s.pins == nil {
		return PinState{AttemptsLeft: -1, SessionLocked: !unlocked}
	}
	record, configured, err := s.pins.Load()
	if err != nil || !configured {
		return PinState{AttemptsLeft: -1, SessionLocked: !unlocked}
	}
	state := pinStateFromRecord(record, true, time.Now())
	state.AutoLock = autoLock
	state.SessionLocked = state.Enabled && !unlocked
	return state
}

// checkAutoLock 在已解锁且开启自动锁定时，若空闲时间达到 15 分钟则锁定会话。
// 返回本次调用是否刚刚触发了自动锁定。
func (s *FolderService) checkAutoLock() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unlocked && s.autoLock && !s.unlockedAt.IsZero() && time.Since(s.unlockedAt) >= pinAutoLockIdle {
		s.unlocked = false
		return true
	}
	return false
}

// requireUnlocked 确保本次会话已通过打开密码。
func (s *FolderService) requireUnlocked() error {
	if s.checkAutoLock() {
		s.appendLog("WARN", "空闲超时，已自动锁定")
	}
	s.mu.Lock()
	unlocked := s.unlocked
	s.mu.Unlock()
	if !unlocked {
		return fmt.Errorf("请先输入打开密码")
	}
	return nil
}

// guardUnlocked 与 requireUnlocked 相同，但会写入日志。
func (s *FolderService) guardUnlocked() error {
	err := s.requireUnlocked()
	if err != nil {
		s.appendLog("ERROR", err.Error())
	}
	return err
}

// GetPinState 返回打开密码状态，不返回密文或明文密码。
// 同时惰性检查自动锁定，供前端定时轮询感知会话锁定状态。
func (s *FolderService) GetPinState() PinState {
	if s.checkAutoLock() {
		s.appendLog("WARN", "空闲超时，已自动锁定")
	}
	return s.pinState()
}

// ConfigurePin 在当前已解锁会话中设置或修改打开密码。
func (s *FolderService) ConfigurePin(pin string, length int, policy string) PinResult {
	if err := s.requireUnlocked(); err != nil {
		return newPinResult("locked", s.pinState())
	}
	if !validPin(pin, length) {
		return newPinResult("format", s.pinState())
	}
	nonce, ciphertext, err := sealPIN(pin)
	if err != nil {
		s.appendLog("ERROR", err.Error())
		return newPinResult("error", s.pinState())
	}
	s.mu.Lock()
	autoLock := s.autoLock
	s.mu.Unlock()
	record := pinRecord{
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Length:     length,
		Policy:     normalizePinPolicy(policy),
		AutoLock:   autoLock,
	}
	if err := s.pins.Save(record); err != nil {
		s.appendLog("ERROR", err.Error())
		return newPinResult("error", s.pinState())
	}
	s.mu.Lock()
	s.unlocked = true
	s.unlockedAt = time.Now()
	s.mu.Unlock()
	s.appendLog("INFO", "打开密码已设置")
	return newPinResult("ok", s.pinState())
}

// DisablePin 在当前已解锁会话中关闭打开密码。
// locale 由前端传入，用于选择"密码已删除"原生提示框的三语文案。
func (s *FolderService) DisablePin(locale string) PinResult {
	if err := s.requireUnlocked(); err != nil {
		return newPinResult("locked", s.pinState())
	}
	if err := s.pins.Clear(); err != nil {
		s.appendLog("ERROR", err.Error())
		return newPinResult("error", s.pinState())
	}
	s.mu.Lock()
	s.unlocked = true
	s.autoLock = false
	s.unlockedAt = time.Now()
	s.mu.Unlock()
	s.appendLog("INFO", "打开密码已关闭")
	showPinDeletedNotice(pinDeletedNoticeFor(locale))
	return newPinResult("ok", s.pinState())
}

// Unlock 校验 PIN；成功则解锁本次会话，失败按策略记录次数或锁定。
func (s *FolderService) Unlock(pin string) PinResult {
	state := s.pinState()
	if !state.Enabled {
		s.mu.Lock()
		s.unlocked = true
		s.unlockedAt = time.Now()
		s.mu.Unlock()
		return newPinResult("disabled", state)
	}
	if state.Locked {
		return newPinResult("locked", state)
	}

	s.mu.Lock()
	record, configured, err := s.pins.Load()
	if err != nil || !configured {
		s.mu.Unlock()
		return newPinResult("error", s.pinState())
	}
	if !validPin(pin, record.Length) {
		s.mu.Unlock()
		return newPinResult("format", pinStateFromRecord(record, true, time.Now()))
	}
	ok, err := openPIN(record, pin)
	if err != nil {
		s.mu.Unlock()
		s.appendLog("ERROR", err.Error())
		return newPinResult("error", s.pinState())
	}
	if !ok {
		now := time.Now()
		recordPinFailure(&record, now)
		if err := s.pins.Save(record); err != nil {
			s.mu.Unlock()
			s.appendLog("ERROR", err.Error())
			return newPinResult("error", s.pinState())
		}
		s.unlocked = false
		s.mu.Unlock()
		next := pinStateFromRecord(record, true, time.Now())
		if next.Locked {
			s.appendLog("WARN", "密码错误次数过多，已锁定")
		} else {
			s.appendLog("WARN", "打开密码错误")
		}
		return newPinResult("mismatch", next)
	}

	resetPinFailures(&record)
	if err := s.pins.Save(record); err != nil {
		s.mu.Unlock()
		s.appendLog("ERROR", err.Error())
		return newPinResult("error", s.pinState())
	}
	s.unlocked = true
	s.autoLock = record.AutoLock
	s.unlockedAt = time.Now()
	s.mu.Unlock()
	s.appendLog("INFO", "已解锁")
	return newPinResult("ok", s.pinState())
}

// SetAutoLock 开启或关闭空闲自动锁定。
// 开启时弹出原生询问框：选"是"立即锁定，选"否"再弹提示框并在 15 分钟后锁定。
// locale 由前端传入，用于选择原生对话框文案。
func (s *FolderService) SetAutoLock(enabled bool, locale string) PinResult {
	if !enabled {
		if err := s.saveAutoLock(false); err != nil {
			s.appendLog("ERROR", err.Error())
			return newPinResult("error", s.pinState())
		}
		s.appendLog("INFO", "已关闭空闲自动锁定")
		return newPinResult("ok", s.pinState())
	}

	text := autoLockDialogTextFor(locale)
	immediate := askImmediateLock(text)
	if immediate {
		if err := s.saveAutoLock(true); err != nil {
			s.appendLog("ERROR", err.Error())
			return newPinResult("error", s.pinState())
		}
		s.mu.Lock()
		s.unlocked = false
		s.unlockedAt = time.Now()
		s.mu.Unlock()
		s.appendLog("INFO", "已开启空闲自动锁定并立即锁定")
		return newPinResult("ok", s.pinState())
	}

	showAutoLockNotice(text)
	if err := s.saveAutoLock(true); err != nil {
		s.appendLog("ERROR", err.Error())
		return newPinResult("error", s.pinState())
	}
	s.mu.Lock()
	s.unlockedAt = time.Now()
	s.mu.Unlock()
	s.appendLog("INFO", "已开启空闲自动锁定，15 分钟后锁定")
	return newPinResult("ok", s.pinState())
}

// saveAutoLock 将自动锁定开关持久化到 pin.json 并同步内存状态。
func (s *FolderService) saveAutoLock(enabled bool) error {
	record, configured, err := s.pins.Load()
	if err != nil {
		return err
	}
	if !configured {
		return fmt.Errorf("请先设置打开密码")
	}
	record.AutoLock = enabled
	if err := s.pins.Save(record); err != nil {
		return err
	}
	s.mu.Lock()
	s.autoLock = enabled
	if !enabled {
		s.unlockedAt = time.Time{}
	}
	s.mu.Unlock()
	return nil
}

// Lock 手动锁定当前会话，需重新输入打开密码才能继续使用。
func (s *FolderService) Lock() PinResult {
	s.mu.Lock()
	s.unlocked = false
	s.unlockedAt = time.Time{}
	s.mu.Unlock()
	s.appendLog("INFO", "已手动锁定")
	return newPinResult("ok", s.pinState())
}

// askImmediateLock 弹出原生问答框，返回用户是否选择"是"（立即锁定）。
func askImmediateLock(text autoLockDialogText) bool {
	app := application.Get()
	if app == nil {
		return false
	}
	answer := false
	dialog := app.Dialog.Question().SetTitle(text.Title).SetMessage(text.Question)
	yes := dialog.AddButton(text.Yes)
	no := dialog.AddButton(text.No)
	dialog.SetDefaultButton(no)
	dialog.SetCancelButton(no)
	yes.OnClick(func() { answer = true })
	no.OnClick(func() { answer = false })
	dialog.Show()
	return answer
}

// showAutoLockNotice 弹出只有一个"确定"按钮的原生提示框。
func showAutoLockNotice(text autoLockDialogText) {
	showNativeNotice(text.InfoTitle, text.InfoMessage)
}

// showPinDeletedNotice 弹出"密码已删除"原生提示框。
func showPinDeletedNotice(text pinDeletedNotice) {
	showNativeNotice(text.Title, text.Message)
}

// showNativeNotice 弹出只有一个"确定"按钮的原生提示框。
func showNativeNotice(title, message string) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Dialog.Info().SetTitle(title).SetMessage(message).Show()
}

// appendLog 记录日志并向前端推送事件。
func (s *FolderService) appendLog(level, message string) {
	entry := fmt.Sprintf("%s [%s] %s", time.Now().Format("15:04:05"), level, message)
	s.mu.Lock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > 200 {
		s.logs = s.logs[len(s.logs)-200:]
	}
	s.mu.Unlock()

	if s.fileLog != nil {
		s.fileLog.Log(slogLevelFor(level), message)
	}
	if app := application.Get(); app != nil {
		app.Event.Emit("folder-log", entry)
	}
}

// trace 只写文件日志，不打扰界面日志区。
func (s *FolderService) trace(message string) {
	if s.fileLog != nil {
		s.fileLog.Log(slogLevelTrace, message)
	}
}

// debug 只写文件日志，不打扰界面日志区。
func (s *FolderService) debug(message string) {
	if s.fileLog != nil {
		s.fileLog.Log(slog.LevelDebug, message)
	}
}

// EncryptFolder 隐藏文件夹。若填写了新名称，先改名再隐藏。
func (s *FolderService) EncryptFolder(path, newName string) error {
	if err := s.guardUnlocked(); err != nil {
		return err
	}
	if isProtectedPath(path) {
		err := fmt.Errorf("拒绝操作系统目录或程序自身目录: %s", path)
		s.appendLog("ERROR", err.Error())
		return err
	}

	hiddenPath, err := renameThenHide(path, newName)
	if err != nil {
		s.appendLog("ERROR", err.Error())
		return err
	}
	if strings.TrimSpace(newName) != "" {
		s.appendLog("INFO", "已重命名为: "+strings.TrimSpace(newName))
	}
	s.appendLog("INFO", "已加密: "+hiddenPath)
	return nil
}

// DecryptFolder 将隐藏后的文件夹恢复为资源管理器可访问的原名。
func (s *FolderService) DecryptFolder(path string) error {
	if err := s.guardUnlocked(); err != nil {
		return err
	}
	restoredPath, err := showFolder(path)
	if err != nil {
		s.appendLog("ERROR", err.Error())
		return err
	}
	s.appendLog("INFO", "已解密: "+restoredPath)
	return nil
}

// EncryptFolderByACL 使用方案 D 隐藏文件夹：备份 DACL，加密写入内部 .DS_Store，并拒绝当前用户列目录。
func (s *FolderService) EncryptFolderByACL(path string) error {
	if err := s.guardUnlocked(); err != nil {
		return err
	}
	if isProtectedPath(path) {
		err := fmt.Errorf("拒绝操作系统目录或程序自身目录: %s", path)
		s.appendLog("ERROR", err.Error())
		return err
	}
	if err := hideWithACL(path); err != nil {
		s.appendLog("ERROR", err.Error())
		return err
	}
	s.appendLog("INFO", "已用 ACL 加密: "+path)
	s.appendLog("INFO", "权限备份已加密写入文件夹内部 "+dsStoreName)
	return nil
}

// DecryptFolderByACL 读取内部 .DS_Store 恢复 DACL，使资源管理器可以再次打开目录。
func (s *FolderService) DecryptFolderByACL(path string) error {
	if err := s.guardUnlocked(); err != nil {
		return err
	}
	if isProtectedPath(path) {
		err := fmt.Errorf("拒绝操作系统目录或程序自身目录: %s", path)
		s.appendLog("ERROR", err.Error())
		return err
	}
	warning, err := showWithACL(path)
	if err != nil {
		s.appendLog("ERROR", err.Error())
		return err
	}
	s.appendLog("INFO", "已解密: "+path)
	if warning != "" {
		s.appendLog("WARN", warning)
	}
	return nil
}

// EncryptFileToJunx 把单个文件打包为 .junx。
// outputName 为空时使用源文件名；勾选 deleteOriginal 时在校验通过后永久删除源文件。
func (s *FolderService) EncryptFileToJunx(path, outputName string, deleteOriginal bool, locale string) error {
	return s.encryptToJunx(path, junxFileKind, outputName, deleteOriginal, locale)
}

// EncryptFolderToJunx 把整个目录树打包为 .junx，目录名与内部文件名一并加密。
func (s *FolderService) EncryptFolderToJunx(path, outputName string, deleteOriginal bool, locale string) error {
	return s.encryptToJunx(path, junxFolderKind, outputName, deleteOriginal, locale)
}

// EncryptPath 自动识别路径类型并打包为 .junx。
// 普通文件与文件夹共用同一入口；已是 .junx 的容器请使用 DecryptJunx。
func (s *FolderService) EncryptPath(path, outputName string, deleteOriginal bool, locale string) error {
	probe := probePath(path)
	if !probe.Exists {
		return s.fail(fmt.Errorf("路径不存在: %s", path))
	}
	switch probe.Kind {
	case "junx":
		return s.fail(fmt.Errorf("%s 已是加密容器，请使用解密", probe.Path))
	case junxFileKind:
		return s.encryptToJunx(probe.Path, junxFileKind, outputName, deleteOriginal, locale)
	case junxFolderKind:
		return s.encryptToJunx(probe.Path, junxFolderKind, outputName, deleteOriginal, locale)
	default:
		return s.fail(fmt.Errorf("不支持的路径类型: %s", probe.Path))
	}
}

// DecryptJunx 通用解密：读取容器清单后自动决定还原单个文件还是整个目录树。
// 加密页与批量解密都调用它，用户无需事先知道容器里装的是什么。
func (s *FolderService) DecryptJunx(junxPath string) error {
	return s.decryptFromJunx(junxPath)
}

// encryptToJunx 加密主流程：扫描 → 打包 → 封装 → 写盘 → 回读校验 → 可选删除源。
func (s *FolderService) encryptToJunx(src, kind, outputName string, deleteOriginal bool, locale string) error {
	if err := s.guardUnlocked(); err != nil {
		return err
	}
	src = strings.TrimSpace(src)
	if src == "" {
		return s.fail(fmt.Errorf("请选择要加密的路径"))
	}
	if isProtectedPath(src) {
		return s.fail(fmt.Errorf("拒绝操作系统目录或程序自身目录: %s", src))
	}

	info, err := os.Stat(src)
	if err != nil {
		return s.fail(fmt.Errorf("读取源路径失败: %w", err))
	}
	if kind == junxFileKind && !info.Mode().IsRegular() {
		return s.fail(fmt.Errorf("不是普通文件: %s", src))
	}
	if kind == junxFolderKind && !info.IsDir() {
		return s.fail(fmt.Errorf("不是文件夹: %s", src))
	}

	absSrc, err := filepath.Abs(src)
	if err != nil {
		return s.fail(err)
	}
	outPath, err := junxOutputPath(absSrc, outputName)
	if err != nil {
		return s.fail(err)
	}
	if samePath(absSrc, outPath) {
		return s.fail(fmt.Errorf("输出路径不能与源路径相同"))
	}
	if kind == junxFolderKind && isInsidePath(absSrc, outPath) {
		return s.fail(fmt.Errorf("输出文件不能放在源文件夹内部"))
	}
	if _, statErr := os.Stat(outPath); statErr == nil {
		return s.fail(fmt.Errorf("目标文件已存在，请换一个输出名称: %s", outPath))
	}
	s.debug(fmt.Sprintf("加密准备: kind=%s src=%s out=%s", kind, absSrc, outPath))

	emit := s.junxEmitter()
	emit("scan", 0, filepath.Base(absSrc))
	payload, err := buildJunxPayload(absSrc, kind, emit)
	if err != nil {
		return s.fail(err)
	}

	s.debug(fmt.Sprintf("扫描完成: 文件=%d 目录=%d 跳过=%d", payload.files, payload.dirs, payload.skipped))
	plain, err := packJunxPayload(payload.manifest, payload.body)
	if err != nil {
		return s.fail(err)
	}
	emit("encrypt", 74, filepath.Base(absSrc))
	sealed, err := sealJunxData(plain, markForKind(payload.manifest.Kind))
	if err != nil {
		return s.fail(err)
	}

	// 先写临时文件，校验通过后再改名，避免中途失败留下半个容器。
	tmpPath := outPath + ".tmp"
	emit("write", 86, filepath.Base(outPath))
	if err := os.WriteFile(tmpPath, sealed, 0o600); err != nil {
		return s.fail(fmt.Errorf("写入失败: %w", err))
	}
	emit("verify", 93, filepath.Base(outPath))
	if err := verifyJunxFile(tmpPath, payload); err != nil {
		_ = os.Remove(tmpPath)
		return s.fail(err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return s.fail(fmt.Errorf("保存失败: %w", err))
	}

	if kind == junxFileKind {
		s.appendLog("INFO", "已加密: "+outPath)
	} else {
		s.appendLog("INFO", fmt.Sprintf("已加密: %s（%d 个文件，%d 个目录）", outPath, payload.files, payload.dirs))
	}
	if payload.skipped > 0 {
		s.appendLog("WARN", fmt.Sprintf("已跳过 %d 个符号链接或特殊文件", payload.skipped))
	}

	if deleteOriginal {
		if isUnderUserProfile(absSrc) {
			// 后端兜底：个人文件夹即使前端状态被绕过也不自动删除。
			s.appendLog("WARN", "高风险个人目录不允许自动删除，已保留原路径，请在加密完成后手动删除: "+absSrc)
		} else if askDeleteOriginal(locale, filepath.Base(absSrc), kind == junxFolderKind) {
			if err := os.RemoveAll(absSrc); err != nil {
				return s.fail(fmt.Errorf("删除原路径失败: %w", err))
			}
			s.appendLog("WARN", "已永久删除原路径: "+absSrc)
		} else {
			s.appendLog("INFO", "已保留原路径: "+absSrc)
		}
	}

	emit("done", 100, "")
	return nil
}

// decryptFromJunx 解密主流程：读容器 → 校验类型 → 按 manifest 重建。
func (s *FolderService) decryptFromJunx(junxPath string) error {
	if err := s.guardUnlocked(); err != nil {
		return err
	}
	junxPath = strings.TrimSpace(junxPath)
	if junxPath == "" {
		return s.fail(fmt.Errorf("请选择要解密的 .junx 文件"))
	}
	if isProtectedPath(junxPath) {
		return s.fail(fmt.Errorf("拒绝操作系统目录或程序自身目录: %s", junxPath))
	}
	info, err := os.Stat(junxPath)
	if err != nil {
		return s.fail(fmt.Errorf("读取容器失败: %w", err))
	}
	if !info.Mode().IsRegular() {
		return s.fail(fmt.Errorf("不是 .junx 文件: %s", junxPath))
	}
	absPath, err := filepath.Abs(junxPath)
	if err != nil {
		return s.fail(err)
	}

	emit := s.junxEmitter()
	emit("scan", 0, filepath.Base(absPath))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return s.fail(fmt.Errorf("读取容器失败: %w", err))
	}
	emit("decrypt", 30, filepath.Base(absPath))

	manifest, body, mark, err := openJunxData(data)
	if err != nil {
		return s.fail(err)
	}
	s.debug(fmt.Sprintf("容器解析: kind=%s name=%s mark=%d", manifest.Kind, manifest.Name, mark))
	// 标记只作提示：与清单不一致时以清单为准，并记录一次告警。
	if mark != junxMarkUnknown && mark != markForKind(manifest.Kind) {
		s.appendLog("WARN", "容器类型标记与内容不一致，已按清单还原")
	}

	emit("extract", 50, manifest.Name)
	target, err := unpackJunxPayload(manifest, body, filepath.Dir(absPath), emit)
	if err != nil {
		return s.fail(err)
	}
	s.appendLog("INFO", "已解密到: "+target)
	emit("done", 100, "")
	return nil
}

// verifyJunxFile 回读刚写出的临时容器，确认能解密且清单与内存一致。
func verifyJunxFile(path string, payload *junxPayload) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("校验失败：无法回读: %w", err)
	}
	manifest, body, mark, err := openJunxData(data)
	if err != nil {
		return fmt.Errorf("校验失败：%w", err)
	}
	if mark != markForKind(manifest.Kind) {
		return fmt.Errorf("校验失败：容器类型标记不一致")
	}
	if manifest.Kind != payload.manifest.Kind || manifest.Name != payload.manifest.Name {
		return fmt.Errorf("校验失败：容器信息与源不一致")
	}
	if len(manifest.Entries) != len(payload.manifest.Entries) {
		return fmt.Errorf("校验失败：条目数量不一致")
	}
	if len(body) != len(payload.body) {
		return fmt.Errorf("校验失败：内容长度不一致")
	}
	return validateJunxManifest(manifest, body)
}

// junxEmitter 返回向前端推送 folder-progress 事件的回调；无 GUI 时静默跳过。
func (s *FolderService) junxEmitter() junxProgressFunc {
	return func(phase string, percent int, current string) {
		if app := application.Get(); app != nil {
			app.Event.Emit("folder-progress", JunxProgress{Percent: percent, Phase: phase, Current: current})
		}
	}
}

// fail 记录错误日志并原样返回，保持各接口的错误处理风格一致。
func (s *FolderService) fail(err error) error {
	s.appendLog("ERROR", err.Error())
	return err
}

// samePath 按 Windows 习惯忽略大小写比较两个绝对路径。
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// isInsidePath 判断 path 是否位于 dir 内部。
func isInsidePath(dir, path string) bool {
	base := strings.ToLower(filepath.Clean(dir)) + string(os.PathSeparator)
	return strings.HasPrefix(strings.ToLower(filepath.Clean(path)), base)
}

// askDeleteOriginal 弹出原生二次确认框，默认按钮为"保留"。
func askDeleteOriginal(locale, name string, isFolder bool) bool {
	app := application.Get()
	if app == nil {
		return false
	}
	text := deleteOriginalTextFor(locale, name, isFolder)
	answer := false
	dialog := app.Dialog.Question().SetTitle(text.Title).SetMessage(text.Question)
	yes := dialog.AddButton(text.Yes)
	no := dialog.AddButton(text.No)
	dialog.SetDefaultButton(no)
	dialog.SetCancelButton(no)
	yes.OnClick(func() { answer = true })
	no.OnClick(func() { answer = false })
	dialog.Show()
	return answer
}

// RecentLogs 返回最近的日志。
func (s *FolderService) RecentLogs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.logs...)
}

// ClearLogs 清空内存日志。
func (s *FolderService) ClearLogs() {
	s.mu.Lock()
	s.logs = s.logs[:0]
	s.mu.Unlock()
}

// GetConfigInfo 返回前端展示用的配置摘要。
func (s *FolderService) GetConfigInfo() ConfigInfo {
	cfg, err := s.config.Load()
	if err != nil {
		cfg = defaultAppConfig()
	}
	info := ConfigInfo{
		Language:       cfg.Language,
		AlwaysOnTop:    cfg.AlwaysOnTop,
		BatchFile:      cfg.BatchFile,
		BatchFolder:    cfg.BatchFolder,
		LogLevel:       cfg.LogLevel,
		ConfigPath:     s.config.path,
		LogDir:         defaultLogDir(),
		LogFilePath:    currentLogPath(),
		NeedsMigration: !cfg.LegacyMigrated,
	}
	if s.fileLog != nil {
		info.LogDir = s.fileLog.Dir()
		info.LogFilePath = s.fileLog.CurrentPath()
	}
	return info
}

// MigrateLegacyConfig 把旧版 localStorage 偏好写入 config.jsonc。
func (s *FolderService) MigrateLegacyConfig(language string, alwaysOnTop, batchFile, batchFolder bool) error {
	return s.mutateConfig(func(cfg *AppConfig) {
		cfg.Language = normalizeAppLanguage(language)
		cfg.AlwaysOnTop = alwaysOnTop
		cfg.BatchFile = batchFile
		cfg.BatchFolder = batchFolder
		cfg.LegacyMigrated = legacyConfigMigrated
	})
}

func (s *FolderService) mutateConfig(mutate func(*AppConfig)) error {
	if s.config == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	mutate(&cfg)
	return s.config.Save(cfg)
}

// ResetConfig 把 config.jsonc 恢复为默认内容，并清除锁屏密码。
// 配置文件路径本身保持不变；开机启动等注册表设置也不受影响。
func (s *FolderService) ResetConfig() error {
	if s.config == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	s.configMu.Lock()
	cfg := defaultAppConfig()
	cfg.LegacyMigrated = true
	cfg.LegacyPinMigrated = true
	if err := s.config.Save(cfg); err != nil {
		s.configMu.Unlock()
		return err
	}
	s.configMu.Unlock()

	if s.fileLog != nil {
		s.fileLog.SetLevel(cfg.LogLevel)
	}
	s.mu.Lock()
	s.unlocked = true
	s.autoLock = false
	s.unlockedAt = time.Now()
	s.mu.Unlock()
	s.appendLog("INFO", "配置已恢复默认")
	return nil
}

// SetLanguage 持久化显示语言。
func (s *FolderService) SetLanguage(language string) error {
	return s.mutateConfig(func(cfg *AppConfig) {
		cfg.Language = normalizeAppLanguage(language)
	})
}

// SetAlwaysOnTop 持久化窗口置顶状态；实际窗口状态由前端设置。
func (s *FolderService) SetAlwaysOnTop(enabled bool) error {
	return s.mutateConfig(func(cfg *AppConfig) {
		cfg.AlwaysOnTop = enabled
	})
}

// SetBatchSettings 持久化两个批量开关。
func (s *FolderService) SetBatchSettings(file, folder bool) error {
	return s.mutateConfig(func(cfg *AppConfig) {
		cfg.BatchFile = file
		cfg.BatchFolder = folder
	})
}

// SetLogLevel 持久化日志级别，并立即应用到文件日志。
func (s *FolderService) SetLogLevel(level string) error {
	normalized := normalizeLogLevel(level)
	if err := s.mutateConfig(func(cfg *AppConfig) {
		cfg.LogLevel = normalized
	}); err != nil {
		return err
	}
	if s.fileLog != nil {
		s.fileLog.SetLevel(normalized)
	}
	return nil
}

// ChangeConfigPath 把 config.jsonc 迁移到新目录，并更新注册表指针。
func (s *FolderService) ChangeConfigPath(dir string) error {
	if s.config == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	targetDir, err := filepath.Abs(strings.TrimSpace(dir))
	if err != nil {
		return err
	}
	if targetDir == "" {
		return fmt.Errorf("请选择配置目录")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(targetDir, configFileName)
	s.configMu.Lock()
	defer s.configMu.Unlock()
	old := s.config.path
	if samePath(old, target) {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("目标位置已存在 %s", configFileName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg, err := s.config.Load()
	if err != nil {
		return err
	}
	if err := s.config.writeTo(target, cfg); err != nil {
		return err
	}
	if err := writeConfigPath(target); err != nil {
		_ = os.Remove(target)
		return err
	}
	s.config.path = target
	if s.pins != nil && s.pins.config != nil {
		s.pins.config.path = target
	}
	_ = os.Remove(old)
	return nil
}

// OpenConfigFile 用默认文本编辑器打开 config.jsonc。
func (s *FolderService) OpenConfigFile() error {
	if s.config == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	if _, err := os.Stat(s.config.path); errors.Is(err, os.ErrNotExist) {
		if err := s.config.ensure(); err != nil {
			return err
		}
	}
	return openWithDefaultApp(s.config.path)
}

// OpenConfigFolder 在资源管理器中打开配置文件所在目录。
func (s *FolderService) OpenConfigFolder() error {
	if s.config == nil {
		return fmt.Errorf("配置服务未初始化")
	}
	return openContainingFolder(s.config.path)
}

// OpenLogFile 用默认文本编辑器打开当天日志文件。
func (s *FolderService) OpenLogFile() error {
	if s.fileLog == nil {
		return fmt.Errorf("日志服务未初始化")
	}
	if err := s.fileLog.EnsureCurrent(); err != nil {
		return err
	}
	return openWithDefaultApp(s.fileLog.CurrentPath())
}

// OpenLogFolder 打开固定日志目录，供用户查看或手动删除。
func (s *FolderService) OpenLogFolder() error {
	dir := defaultLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return openWithDefaultApp(dir)
}

// GetStartupState 返回开机启动与管理员运行设置。
func (s *FolderService) GetStartupState() StartupState {
	return readStartupState()
}

// SetAutoStart 启用或关闭开机启动；关闭时同时清除管理员运行标志。
func (s *FolderService) SetAutoStart(enabled bool) error {
	if err := writeAutoStart(enabled); err != nil {
		return err
	}
	if !enabled {
		return writeRunAsAdmin(false)
	}
	return nil
}

// SetRunAsAdmin 设置是否以管理员身份启动，仅允许在开机启动已启用时开启。
func (s *FolderService) SetRunAsAdmin(enabled bool) error {
	if enabled && !readStartupState().AutoStart {
		return fmt.Errorf("请先启用开机启动")
	}
	return writeRunAsAdmin(enabled)
}

// ClearRecentFiles 清除当前用户的最近文件记录，不删除原文件。
func (s *FolderService) ClearRecentFiles() error {
	if err := clearRecentFiles(); err != nil {
		return s.fail(err)
	}
	s.appendLog("INFO", "已清除最近使用的文件记录")
	return nil
}

// ShowDesktopIcons 在桌面显示此电脑、控制面板、网络、用户文件与回收站。
func (s *FolderService) ShowDesktopIcons() error {
	if err := showDesktopIcons(); err != nil {
		return s.fail(err)
	}
	s.appendLog("INFO", "已在桌面显示此电脑/控制面板/网络/用户文件/回收站")
	return nil
}

// AlignTaskbarLeft 将 Win11 任务栏与开始按钮左对齐。
func (s *FolderService) AlignTaskbarLeft() error {
	if err := alignTaskbarLeft(); err != nil {
		return s.fail(err)
	}
	s.appendLog("INFO", "已将任务栏与开始按钮左对齐")
	return nil
}

// ShowHiddenFiles 显示隐藏文件与扩展名，但不显示受保护的系统文件。
func (s *FolderService) ShowHiddenFiles() error {
	if err := showHiddenFiles(); err != nil {
		return s.fail(err)
	}
	s.appendLog("INFO", "已显示隐藏文件与扩展名，不显示受保护的系统文件")
	return nil
}

// RemoveShortcutArrow 去掉桌面快捷方式小箭头，需要管理员权限。
func (s *FolderService) RemoveShortcutArrow() error {
	if err := removeShortcutArrow(); err != nil {
		return s.fail(err)
	}
	s.appendLog("INFO", "已去掉桌面快捷方式的小箭头")
	return nil
}

// guardPathLower 把路径转成用于保护判定的小写绝对路径。
// 解析符号链接可防止 junction 把个人目录指向系统目录后绕过检查。
func guardPathLower(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return strings.ToLower(filepath.Clean(abs))
}

// isProtectedPath 禁止系统盘关键目录、程序所在目录和驱动器根目录。
// Users 根目录本身禁止加密，但其下的个人文件夹允许（由删除风险逻辑单独保护）。
func isProtectedPath(path string) bool {
	clean := guardPathLower(path)
	sep := string(os.PathSeparator)

	if exe, err := os.Executable(); err == nil {
		exeDir := strings.ToLower(filepath.Dir(exe))
		if clean == exeDir || strings.HasPrefix(clean, exeDir+sep) {
			return true
		}
	}

	drive := strings.TrimSpace(os.Getenv("SystemDrive"))
	if drive == "" {
		drive = "C:"
	}
	drive = strings.TrimSuffix(strings.ToLower(drive), sep)

	protected := []string{
		drive + sep + `windows`,
		drive + sep + `program files`,
		drive + sep + `program files (x86)`,
		drive + sep + `programdata`,
		drive + sep + `amd`,
		drive + sep + `recovery`,
	}
	for _, item := range protected {
		low := strings.ToLower(item)
		if clean == low || strings.HasPrefix(clean, low+sep) {
			return true
		}
	}

	// 只拒绝 Users 根目录，不拒绝 C:\Users\<个人文件夹>。
	if clean == drive+sep+`users` {
		return true
	}

	if len(clean) == 3 && clean[1] == ':' && clean[2] == '\\' {
		return true
	}
	return false
}

// isUnderHome 判断路径是否位于给定个人目录（含其本身）。
func isUnderHome(path, home string) bool {
	clean := guardPathLower(path)
	root := strings.ToLower(filepath.Clean(home))
	return clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator))
}

// isUnderUserProfile 判断路径是否位于当前用户的个人目录树。
func isUnderUserProfile(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return false
	}
	return isUnderHome(path, home)
}

// DeleteRisk 是给前端展示的删除风险判定结果。
type DeleteRisk struct {
	HighRisk bool   `json:"highRisk"`
	Reason   string `json:"reason"`
}

// CheckDeleteRisk 判断目标路径删除风险。
// 个人文件夹允许加密，但不应自动删除；Reason 使用稳定标识，文案由前端本地化。
func (s *FolderService) CheckDeleteRisk(path string) DeleteRisk {
	if isUnderUserProfile(path) {
		return DeleteRisk{HighRisk: true, Reason: "personal"}
	}
	return DeleteRisk{}
}
