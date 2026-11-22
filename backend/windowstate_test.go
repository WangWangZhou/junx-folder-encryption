package backend

import (
	"os"
	"path/filepath"
	"testing"
)

// useTempWindowStatePath 在测试期间把窗口状态文件路径指向临时目录。
func useTempWindowStatePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), windowStateFileName)
	old := windowStatePathOverride
	windowStatePathOverride = path
	t.Cleanup(func() { windowStatePathOverride = old })
	return path
}

func TestWindowStateSaveLoadRoundTrip(t *testing.T) {
	path := useTempWindowStatePath(t)

	want := WindowState{
		Version:   windowStateVersion,
		X:         120,
		Y:         80,
		Width:     576,
		Height:    470,
		MonitorID: "DISPLAY1",
	}
	if err := SaveWindowState(want); err != nil {
		t.Fatalf("保存窗口状态失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("窗口状态文件未生成: %v", err)
	}
	got := LoadWindowState()
	if got != want {
		t.Fatalf("读取窗口状态不一致: got %+v, want %+v", got, want)
	}
}

func TestLoadWindowStateMissingOrCorrupt(t *testing.T) {
	path := useTempWindowStatePath(t)

	if got := LoadWindowState(); got != DefaultWindowState() {
		t.Fatalf("文件缺失时应返回默认状态: %+v", got)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadWindowState(); got != DefaultWindowState() {
		t.Fatalf("文件损坏时应返回默认状态: %+v", got)
	}
}

func TestResolveWindowBoundsSameMonitor(t *testing.T) {
	state := WindowState{X: 2100, Y: 220, Width: 576, Height: 470, MonitorID: "DISPLAY2"}
	screens := []ScreenInfo{
		{ID: "DISPLAY1", IsPrimary: true, WorkArea: Rect{X: 0, Y: 0, Width: 1920, Height: 1080}},
		{ID: "DISPLAY2", WorkArea: Rect{X: 1920, Y: 0, Width: 1280, Height: 1024}},
	}

	rect, id := ResolveWindowBounds(state, screens)
	if id != "DISPLAY2" {
		t.Fatalf("应保持在原显示器: got %s", id)
	}
	if rect.X != 2100 || rect.Y != 220 || rect.Width != 576 || rect.Height != 470 {
		t.Fatalf("同屏恢复应沿用原坐标: %+v", rect)
	}
}

func TestResolveWindowBoundsMonitorRemoved(t *testing.T) {
	state := WindowState{X: 2000, Y: 200, Width: 576, Height: 470, MonitorID: "DISPLAY2"}
	// 副屏已被拔掉，只剩主屏。
	screens := []ScreenInfo{
		{ID: "DISPLAY1", IsPrimary: true, WorkArea: Rect{X: 0, Y: 0, Width: 1920, Height: 1040}},
	}

	rect, id := ResolveWindowBounds(state, screens)
	if id != "DISPLAY1" {
		t.Fatalf("应回退到主屏: got %s", id)
	}
	wantX := (1920 - 576) / 2
	wantY := (1040 - 470) / 2
	if rect.X != wantX || rect.Y != wantY {
		t.Fatalf("屏幕消失后应在主屏居中: got (%d,%d), want (%d,%d)", rect.X, rect.Y, wantX, wantY)
	}
}

func TestResolveWindowBoundsClampsOutOfScreen(t *testing.T) {
	state := WindowState{X: 99999, Y: -99999, Width: 2000, Height: 1200, MonitorID: "DISPLAY1"}
	screens := []ScreenInfo{
		{ID: "DISPLAY1", IsPrimary: true, WorkArea: Rect{X: 0, Y: 0, Width: 1366, Height: 768}},
	}

	rect, _ := ResolveWindowBounds(state, screens)
	if rect.Width != 1366 || rect.Height != 768 {
		t.Fatalf("窗口大小不应超过工作区: %+v", rect)
	}
	// 至少保留 minVisiblePixels 可见。
	if rect.X > screens[0].WorkArea.Width-minVisiblePixels {
		t.Fatalf("窗口 X 越界过多: %d", rect.X)
	}
	if rect.Y < minVisiblePixels-screens[0].WorkArea.Height {
		t.Fatalf("窗口 Y 越界过多: %d", rect.Y)
	}
}

func TestResolveWindowBoundsNoScreens(t *testing.T) {
	state := WindowState{X: 100, Y: 100, Width: 576, Height: 470, MonitorID: "DISPLAY1"}
	rect, id := ResolveWindowBounds(state, nil)
	if id != "" {
		t.Fatalf("无屏幕时显示器 ID 应为空: %s", id)
	}
	if rect.X != 0 || rect.Y != 0 || rect.Width != 576 || rect.Height != 470 {
		t.Fatalf("无屏幕时应使用原点与保存尺寸: %+v", rect)
	}
}

func TestSaveWindowStateRejectsInvalid(t *testing.T) {
	useTempWindowStatePath(t)
	if err := SaveWindowState(WindowState{Width: 100, Height: 100}); err == nil {
		t.Fatal("过小的尺寸应被拒绝")
	}
}
