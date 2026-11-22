package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	logPrefix       = "app-"
	logSuffix       = ".log"
	logRetention    = 7 * 24 * time.Hour
	slogLevelTrace  = slog.Level(-8)
	disabledLogLevel = slog.Level(127)
)

// FileLogger 负责按天写入文件日志，并支持运行时调整级别。
type FileLogger struct {
	mu      sync.Mutex
	level   *slog.LevelVar
	writer  *dailyLogWriter
	logger  *slog.Logger
	levelID string
}

func NewFileLogger(dir, level string) *FileLogger {
	levelVar := new(slog.LevelVar)
	writer := newDailyLogWriter(dir)
	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: levelVar,
	}))
	fl := &FileLogger{
		level:  levelVar,
		writer: writer,
		logger: logger,
	}
	fl.SetLevel(level)
	return fl
}

func (l *FileLogger) SetLevel(level string) {
	if l == nil {
		return
	}
	normalized := normalizeLogLevel(level)
	levelID := normalized
	target := disabledLogLevel
	switch normalized {
	case "trace":
		target = slogLevelTrace
		levelID = "trace"
	case "debug":
		target = slog.LevelDebug
		levelID = "debug"
	case "normal":
		target = slog.LevelInfo
		levelID = "normal"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.levelID = levelID
	l.level.Set(target)
}

func (l *FileLogger) Level() string {
	if l == nil {
		return "normal"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.levelID
}

func (l *FileLogger) Log(level slog.Level, message string) {
	if l == nil || l.logger == nil || l.levelID == "off" {
		return
	}
	l.logger.Log(context.Background(), level, message)
}

func (l *FileLogger) Dir() string {
	if l == nil || l.writer == nil {
		return defaultLogDir()
	}
	return l.writer.dir
}

func (l *FileLogger) CurrentPath() string {
	if l == nil || l.writer == nil {
		return currentLogPath()
	}
	return l.writer.currentPath()
}

func (l *FileLogger) EnsureCurrent() error {
	if l == nil || l.writer == nil {
		return fmt.Errorf("日志服务未初始化")
	}
	return l.writer.ensureCurrent()
}

func (l *FileLogger) Close() error {
	if l == nil || l.writer == nil {
		return nil
	}
	return l.writer.close()
}

func slogLevelFor(name string) slog.Level {
	switch strings.ToUpper(name) {
	case "TRACE":
		return slogLevelTrace
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type dailyLogWriter struct {
	mu          sync.Mutex
	dir         string
	current     string
	file        *os.File
	lastCleanup time.Time
}

func newDailyLogWriter(dir string) *dailyLogWriter {
	w := &dailyLogWriter{dir: dir}
	_ = os.MkdirAll(dir, 0o755)
	_ = w.cleanup()
	return w
}

func (w *dailyLogWriter) pathFor(t time.Time) string {
	return filepath.Join(w.dir, logPrefix+t.Format("2006-01-02")+logSuffix)
}

func (w *dailyLogWriter) currentPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != "" {
		return w.current
	}
	return w.pathFor(time.Now())
}

func (w *dailyLogWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *dailyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureCurrentLocked(); err != nil {
		return 0, err
	}
	if w.file == nil {
		return 0, fmt.Errorf("日志文件未打开")
	}
	return w.file.Write(p)
}

func (w *dailyLogWriter) ensureCurrent() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ensureCurrentLocked()
}

func (w *dailyLogWriter) ensureCurrentLocked() error {
	path := w.pathFor(time.Now())
	if path != w.current {
		if w.file != nil {
			_ = w.file.Close()
			w.file = nil
		}
		w.current = path
	}
	if w.file != nil {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_ = w.file.Close()
		w.file = nil
	}
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.file = file
	if time.Since(w.lastCleanup) >= time.Hour {
		_ = w.cleanupLocked()
		w.lastCleanup = time.Now()
	}
	return nil
}

func (w *dailyLogWriter) cleanup() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cleanupLocked()
}

func (w *dailyLogWriter) cleanupLocked() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-logRetention)
	current := w.pathFor(time.Now())
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, logPrefix) || !strings.HasSuffix(name, logSuffix) {
			continue
		}
		path := filepath.Join(w.dir, name)
		if path == current {
			continue
		}
		dateText := strings.TrimSuffix(strings.TrimPrefix(name, logPrefix), logSuffix)
		date, err := time.ParseInLocation("2006-01-02", dateText, time.Local)
		if err != nil || date.After(cutoff) {
			continue
		}
		_ = os.Remove(path)
	}
	return nil
}
