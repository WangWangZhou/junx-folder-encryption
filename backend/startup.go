package backend

// StartupState 表示开机启动相关设置。
type StartupState struct {
	AutoStart  bool
	RunAsAdmin bool
}
