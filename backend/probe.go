package backend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PathProbe 是批量任务添加前的只读探测结果。
// 前端据此做类型分流（.junx → 解密任务）、同名拦截与嵌套判断。
type PathProbe struct {
	Path          string
	Exists        bool
	IsDir         bool
	Kind          string // file / folder / junx / other / missing
	ContainerKind string // 仅 junx：file / folder / unknown，来自容器头标记
	OutputPath    string // 加密产物路径：源同目录 + 源名 + .junx
	OutputExists  bool
}

// ProbePaths 批量探测拖入的路径，不修改任何文件。
// 与加密/解密接口不同，这里允许在未解锁状态下调用：它只读取名称与文件头。
// outputDir 用于按用户选择的输出目录提前判断加密产物是否已存在。
func (s *FolderService) ProbePaths(paths []string, outputDir string) []PathProbe {
	probes := make([]PathProbe, 0, len(paths))
	for _, path := range paths {
		probe := probePathWithOutput(path, outputDir)
		s.trace(fmt.Sprintf("路径探测: %s -> kind=%s container=%s outputExists=%t", probe.Path, probe.Kind, probe.ContainerKind, probe.OutputExists))
		probes = append(probes, probe)
	}
	return probes
}

func probePath(path string) PathProbe {
	return probePathWithOutput(path, "")
}

func probePathWithOutput(path, outputDir string) PathProbe {
	probe := PathProbe{Path: strings.TrimSpace(path), Kind: "missing"}
	if probe.Path == "" {
		return probe
	}
	if abs, err := filepath.Abs(probe.Path); err == nil {
		probe.Path = abs
	}

	info, err := os.Stat(probe.Path)
	if err != nil {
		return probe
	}
	probe.Exists = true
	probe.IsDir = info.IsDir()

	switch {
	case info.IsDir():
		probe.Kind = "folder"
	case info.Mode().IsRegular():
		if isJunx, mark := junxMarkOf(probe.Path); isJunx {
			probe.Kind = "junx"
			probe.ContainerKind = containerKindName(mark)
		} else {
			probe.Kind = "file"
		}
	default:
		probe.Kind = "other"
	}

	if probe.Kind == "file" || probe.Kind == "folder" {
		if output, outErr := junxOutputPathIn(probe.Path, "", outputDir); outErr == nil {
			probe.OutputPath = output
			probe.OutputExists = junxPathExists(output)
		}
	}
	return probe
}

// junxMarkOf 只读文件头，判断是否为 .junx 容器并取出类型标记。
func junxMarkOf(path string) (bool, byte) {
	file, err := os.Open(path)
	if err != nil {
		return false, junxMarkUnknown
	}
	defer file.Close()

	header := make([]byte, len(junxMagic)+1)
	if _, err := io.ReadFull(file, header); err != nil {
		return false, junxMarkUnknown
	}
	if string(header[:len(junxMagic)]) != junxMagic {
		return false, junxMarkUnknown
	}
	return true, normalizeMark(header[len(junxMagic)])
}

// containerKindName 把标记翻译成前端可读的类型。
func containerKindName(mark byte) string {
	switch mark {
	case junxMarkFile:
		return junxFileKind
	case junxMarkFolder:
		return junxFolderKind
	default:
		return "unknown"
	}
}
