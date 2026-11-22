package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	windowStateFileName = "window-state.json"
	windowStateVersion  = 1

	// DefaultWindowWidth / DefaultWindowHeight 是首次启动时的窗口尺寸。
	DefaultWindowWidth  = 414
	DefaultWindowHeight = 400
	// MinWindowWidth / MinWindowHeight 与 main.go 的窗口最小尺寸保持一致。
	MinWindowWidth  = 380
	MinWindowHeight = 360
	// minVisiblePixels 保证恢复后窗口至少有这么多像素落在工作区内。
	minVisiblePixels = 80
)

// WindowState 是窗口关闭时记住的位置与大小。
// MonitorID 来自屏幕 API 的 Screen.ID，用于多显示器场景下的恢复。
// X / Y 为逻辑像素，Windows 下原点在屏幕左上角。
type WindowState struct {
	Version   int    `json:"version"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MonitorID string `json:"monitorID"`
}

// ScreenInfo 是传给位置校验逻辑的屏幕摘要，对应 Wails Screen 的关键字段。
// 用独立结构而非直接依赖 Wails，方便单元测试构造多屏场景。
type ScreenInfo struct {
	ID        string
	IsPrimary bool
	WorkArea  Rect
}

// Rect 表示逻辑像素下的矩形区域。
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// DefaultWindowState 返回一个无效的初始状态，调用方应据此使用默认窗口尺寸。
func DefaultWindowState() WindowState {
	return WindowState{Version: windowStateVersion, Width: DefaultWindowWidth, Height: DefaultWindowHeight}
}

// IsValid 判断状态是否包含可用的位置与尺寸。
func (s WindowState) IsValid() bool {
	return s.Width >= MinWindowWidth && s.Height >= MinWindowHeight
}

// windowStatePathOverride 仅供测试覆盖路径使用，生产环境始终为空。
var windowStatePathOverride string

// windowStatePath 返回 window-state.json 的路径，固定放在默认配置目录下，
// 不跟随 config.jsonc 的路径迁移，也不受"恢复默认"影响。
func windowStatePath() string {
	if windowStatePathOverride != "" {
		return windowStatePathOverride
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), configDirName)
	} else {
		dir = filepath.Join(dir, configDirName)
	}
	return filepath.Join(dir, windowStateFileName)
}

// LoadWindowState 读取窗口状态；文件不存在或损坏时返回默认状态，不报错。
func LoadWindowState() WindowState {
	data, err := os.ReadFile(windowStatePath())
	if err != nil {
		return DefaultWindowState()
	}
	var state WindowState
	if err := json.Unmarshal(data, &state); err != nil {
		return DefaultWindowState()
	}
	if !state.IsValid() {
		return DefaultWindowState()
	}
	if state.Version != windowStateVersion {
		state.Version = windowStateVersion
	}
	return state
}

// SaveWindowState 原子写入窗口状态。写入失败由调用方记录日志，不影响主流程。
func SaveWindowState(state WindowState) error {
	if !state.IsValid() {
		return errors.New("窗口状态尺寸无效")
	}
	state.Version = windowStateVersion
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("序列化窗口状态失败: %w", err)
	}
	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// ResolveWindowBounds 根据保存的窗口状态与当前屏幕布局，计算恢复后的窗口位置与大小。
// 规则：
//  1. 尺寸钳制到合理范围，且不超过目标屏幕工作区；
//  2. 保存时的显示器仍存在 → 把窗口放回该屏，尽量保持原坐标；
//  3. 显示器已不存在（拔屏 / 布局变化）→ 回退到主屏并按原尺寸居中；
//  4. 位置越界 → 平移到工作区内，确保至少 minVisiblePixels 可见。
//
// 返回的 monitorID 是实际使用的显示器 ID。
func ResolveWindowBounds(state WindowState, screens []ScreenInfo) (Rect, string) {
	width := clampInt(state.Width, MinWindowWidth, 4096)
	height := clampInt(state.Height, MinWindowHeight, 4096)

	if len(screens) == 0 {
		return Rect{X: 0, Y: 0, Width: width, Height: height}, ""
	}

	// 优先匹配保存时的显示器。
	var target *ScreenInfo
	for i := range screens {
		if screens[i].ID != "" && screens[i].ID == state.MonitorID {
			target = &screens[i]
			break
		}
	}
	sameMonitor := target != nil
	if target == nil {
		// 退而求其次：主屏。
		for i := range screens {
			if screens[i].IsPrimary {
				target = &screens[i]
				break
			}
		}
	}
	if target == nil {
		target = &screens[0]
	}

	work := target.WorkArea
	if work.Width <= 0 || work.Height <= 0 {
		work = Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	}
	if width > work.Width {
		width = work.Width
	}
	if height > work.Height {
		height = work.Height
	}

	var x, y int
	if sameMonitor {
		// 同屏：沿用保存的坐标。
		x = state.X
		y = state.Y
	} else {
		// 屏幕已不存在：在新屏幕上居中。
		x = work.X + (work.Width-width)/2
		y = work.Y + (work.Height-height)/2
	}

	// 边界钳制：窗口不能整体跑到工作区外。
	minX := work.X - width + minVisiblePixels
	maxX := work.X + work.Width - minVisiblePixels
	minY := work.Y - height + minVisiblePixels
	maxY := work.Y + work.Height - minVisiblePixels
	x = clampInt(x, minX, maxX)
	y = clampInt(y, minY, maxY)

	return Rect{X: x, Y: y, Width: width, Height: height}, target.ID
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
