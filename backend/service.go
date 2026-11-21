package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// FolderService 暴露给前端的文件夹隐藏服务。
type FolderService struct {
	mu   sync.Mutex
	logs []string
}

// NewFolderService 创建服务实例。
func NewFolderService() *FolderService {
	return &FolderService{logs: []string{}}
}

// ServiceName 返回服务名，供 Wails 日志与绑定生成使用。
func (s *FolderService) ServiceName() string {
	return "FolderService"
}

// Version 返回纯版本号，方案说明由前端按当前语言渲染。
func (s *FolderService) Version() string {
	return "1.2.0"
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

	if app := application.Get(); app != nil {
		app.Event.Emit("folder-log", entry)
	}
}

// EncryptFolder 隐藏文件夹。若填写了新名称，先改名再隐藏。
func (s *FolderService) EncryptFolder(path, newName string) error {
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

// isProtectedPath 禁止操作系统目录、程序所在目录和驱动器根目录。
func isProtectedPath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	sep := string(os.PathSeparator)

	if exe, err := os.Executable(); err == nil {
		exeDir := strings.ToLower(filepath.Dir(exe))
		if clean == exeDir || strings.HasPrefix(clean, exeDir+sep) {
			return true
		}
	}

	protected := []string{
		`C:\Windows`,
		`C:\Program Files`,
		`C:\Program Files (x86)`,
		`C:\ProgramData`,
	}
	for _, item := range protected {
		low := strings.ToLower(item)
		if clean == low || strings.HasPrefix(clean, low+sep) {
			return true
		}
	}

	if len(clean) == 3 && clean[1] == ':' && clean[2] == '\\' {
		return true
	}
	return false
}
