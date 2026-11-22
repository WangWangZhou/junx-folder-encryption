package backend

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileLoggerCreatesFileOnWrite(t *testing.T) {
	dir := t.TempDir()
	logger := NewFileLogger(dir, "normal")
	path := logger.CurrentPath()
	logger.Log(slog.LevelInfo, "first write")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("日志文件未创建: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	logger.Log(slog.LevelInfo, "after delete")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("删除后未重建日志文件: %v", err)
	}
	if !strings.Contains(string(data), "after delete") {
		t.Fatalf("新日志内容不正确: %s", data)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileLoggerOffSkipsFile(t *testing.T) {
	dir := t.TempDir()
	logger := NewFileLogger(dir, "off")
	logger.Log(slog.LevelInfo, "should not write")
	path := logger.CurrentPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("off 级别不应创建日志文件: %v", err)
	}
}

func TestDailyLogWriterCleanup(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "app-2000-01-01.log")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	recent := filepath.Join(dir, "app-"+time.Now().Add(-24*time.Hour).Format("2006-01-02")+".log")
	if err := os.WriteFile(recent, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := newDailyLogWriter(dir)
	if err := writer.cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("过期日志未清理: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("7 天内日志被误删: %v", err)
	}
}
