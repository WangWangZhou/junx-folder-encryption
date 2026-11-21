package backend

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// .junx 容器格式（version 1）
//
//	偏移  长度  内容
//	0     8     magic "JUNX0001"
//	8     12    GCM nonce
//	20    -     AES-256-GCM 密文（含 16 字节 tag）
//
// 明文载荷结构：
//
//	uint32 manifestLen | manifest JSON | 各文件内容按 offset 顺次拼接
//
// 文件名、相对路径、目录树全部写在 manifest 内，而 manifest 属于密文的一部分，
// 因此磁盘上的 .junx 文件无法通过 strings 等工具看出内部名称。
const (
	junxMagic       = "JUNX0001"
	junxExt         = ".junx"
	junxKeySeed     = "folderhide-junx-v1"
	junxFileKind    = "file"
	junxFolderKind  = "folder"
	junxMaxManifest = 64 << 20
	junxChunkSize   = 1 << 20
)

// 容器头第 9 字节的类型标记。仅用于添加任务时提前判断，
// 实际还原始终以加密清单里的 Kind 为准。
const (
	junxMarkUnknown byte = 0x00
	junxMarkFile    byte = 0x01
	junxMarkFolder  byte = 0x02
)

// markForKind 把清单类型映射为容器头标记。
func markForKind(kind string) byte {
	if kind == junxFolderKind {
		return junxMarkFolder
	}
	if kind == junxFileKind {
		return junxMarkFile
	}
	return junxMarkUnknown
}

// normalizeMark 只接受已知取值，其余一律视为未知。
func normalizeMark(mark byte) byte {
	if mark == junxMarkFile || mark == junxMarkFolder {
		return mark
	}
	return junxMarkUnknown
}

// junxProgressFunc 由调用方注入进度回调，避免 crypto 逻辑直接依赖 UI。
type junxProgressFunc func(phase string, percent int, current string)

// junxEntry 描述容器内的一个文件或目录。
type junxEntry struct {
	Path   string `json:"path"` // 相对路径，统一使用 / 分隔；目录以 / 结尾
	Size   int64  `json:"size"`
	Offset int64  `json:"offset"`
	Dir    bool   `json:"dir,omitempty"`
}

// junxManifest 是加密载荷内的目录清单。
type junxManifest struct {
	Kind    string      `json:"kind"` // file 或 folder
	Name    string      `json:"name"` // 原始文件名（含扩展名）或文件夹名
	Entries []junxEntry `json:"entries"`
}

// junxPayload 是打包结果：清单 + 文件内容缓冲区。
type junxPayload struct {
	manifest junxManifest
	body     []byte
	files    int
	dirs     int
	skipped  int // 跳过的符号链接、设备文件等
}

// junxKey 由内置字符串派生 32 字节 AES 密钥。
// 与 metadataKey 使用不同种子，避免两套格式共用同一密钥流。
func junxKey() []byte {
	sum := sha256.Sum256([]byte(junxKeySeed))
	return sum[:]
}

// buildJunxPayload 读取单文件或整个目录树，生成待加密载荷。
// kind 必须是 file 或 folder，且 src 的类型必须与之匹配。
func buildJunxPayload(src, kind string, progress junxProgressFunc) (*junxPayload, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("读取源路径失败: %w", err)
	}
	if kind == junxFileKind {
		return buildFilePayload(src, info, progress)
	}
	if kind == junxFolderKind {
		if !info.IsDir() {
			return nil, fmt.Errorf("不是文件夹: %s", src)
		}
		return buildFolderPayload(src, progress)
	}
	return nil, fmt.Errorf("不支持的加密类型: %s", kind)
}

// buildFilePayload 处理单个文件，manifest 只有一条记录。
func buildFilePayload(src string, info fs.FileInfo, progress junxProgressFunc) (*junxPayload, error) {
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件: %s", src)
	}
	reportProgress(progress, "scan", 0, filepath.Base(src))

	body, err := os.ReadFile(src)
	if err != nil {
		return nil, fmt.Errorf("读取源文件失败: %w", err)
	}
	reportProgress(progress, "build", 70, filepath.Base(src))

	name := filepath.Base(src)
	payload := &junxPayload{
		manifest: junxManifest{
			Kind: junxFileKind,
			Name: name,
			Entries: []junxEntry{{
				Path:   name,
				Size:   int64(len(body)),
				Offset: 0,
			}},
		},
		body:  body,
		files: 1,
	}
	return payload, nil
}

// buildFolderPayload 先统计总量，再打包，保证进度百分比可读。
func buildFolderPayload(root string, progress junxProgressFunc) (*junxPayload, error) {
	reportProgress(progress, "scan", 0, filepath.Base(root))

	var total int64
	var files, dirs, skipped int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			skipped++
			return nil
		}
		if d.IsDir() {
			dirs++
			return nil
		}
		if !d.Type().IsRegular() {
			skipped++
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		total += info.Size()
		files++
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描文件夹失败: %w", err)
	}

	entries := make([]junxEntry, 0, files+dirs)
	body := bytes.NewBuffer(make([]byte, 0, int(minInt64(total, 1<<31))))
	var processed int64

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			entries = append(entries, junxEntry{Path: rel + "/", Dir: true})
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		written, copyErr := appendFileToBuffer(body, path)
		if copyErr != nil {
			return copyErr
		}
		entries = append(entries, junxEntry{Path: rel, Size: written, Offset: processed})
		processed += written
		if total > 0 {
			reportProgress(progress, "build", int(processed*70/total), rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("打包文件夹失败: %w", err)
	}
	reportProgress(progress, "build", 70, filepath.Base(root))

	return &junxPayload{
		manifest: junxManifest{
			Kind:    junxFolderKind,
			Name:    filepath.Base(root),
			Entries: entries,
		},
		body:    body.Bytes(),
		files:   files,
		dirs:    dirs,
		skipped: skipped,
	}, nil
}

// appendFileToBuffer 分块复制文件内容，避免再分配一份完整文件副本。
func appendFileToBuffer(buf *bytes.Buffer, path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}
	defer file.Close()

	chunk := make([]byte, junxChunkSize)
	written, err := io.CopyBuffer(buf, file, chunk)
	if err != nil {
		return written, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}
	return written, nil
}

// packJunxPayload 组装明文载荷：4 字节清单长度 + 清单 + 内容。
func packJunxPayload(manifest junxManifest, body []byte) ([]byte, error) {
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("序列化目录清单失败: %w", err)
	}
	if len(manifestBytes) > junxMaxManifest {
		return nil, fmt.Errorf("目录清单过大，无法打包")
	}
	out := make([]byte, 4+len(manifestBytes)+len(body))
	binary.BigEndian.PutUint32(out[:4], uint32(len(manifestBytes)))
	copy(out[4:], manifestBytes)
	copy(out[4+len(manifestBytes):], body)
	return out, nil
}

// sealJunxData 用 AES-256-GCM 封装明文载荷，输出完整 .junx 字节。
// mark 写入容器头第 9 字节，明文可见但不参与认证，解密时只作为提示。
func sealJunxData(payload []byte, mark byte) ([]byte, error) {
	block, err := aes.NewCipher(junxKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(junxMagic)+1+len(nonce)+len(payload)+gcm.Overhead())
	out = append(out, junxMagic...)
	out = append(out, normalizeMark(mark))
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, payload, nil), nil
}

// openJunxData 校验文件头并解密，返回清单、内容区与类型标记。
// 标记在密文之外，可能被篡改，调用方必须以清单里的 Kind 为准。
func openJunxData(data []byte) (junxManifest, []byte, byte, error) {
	var manifest junxManifest

	block, err := aes.NewCipher(junxKey())
	if err != nil {
		return manifest, nil, junxMarkUnknown, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return manifest, nil, junxMarkUnknown, err
	}

	header := len(junxMagic) + 1 + gcm.NonceSize()
	if len(data) < header+gcm.Overhead() {
		return manifest, nil, junxMarkUnknown, fmt.Errorf("文件过短，不是有效的 .junx 文件")
	}
	if string(data[:len(junxMagic)]) != junxMagic {
		return manifest, nil, junxMarkUnknown, fmt.Errorf("文件头不匹配，不是本工具生成的 .junx 文件")
	}
	mark := normalizeMark(data[len(junxMagic)])
	nonce := data[len(junxMagic)+1 : header]
	plain, err := gcm.Open(nil, nonce, data[header:], nil)
	if err != nil {
		return manifest, nil, junxMarkUnknown, fmt.Errorf("解密失败：文件已损坏或被篡改")
	}
	if len(plain) < 4 {
		return manifest, nil, junxMarkUnknown, fmt.Errorf("载荷过短，目录清单缺失")
	}

	manifestLen := int(binary.BigEndian.Uint32(plain[:4]))
	if manifestLen <= 0 || manifestLen > junxMaxManifest || 4+manifestLen > len(plain) {
		return manifest, nil, mark, fmt.Errorf("目录清单长度异常")
	}
	if err := json.Unmarshal(plain[4:4+manifestLen], &manifest); err != nil {
		return manifest, nil, mark, fmt.Errorf("目录清单格式损坏: %w", err)
	}
	if manifest.Kind != junxFileKind && manifest.Kind != junxFolderKind {
		return manifest, nil, mark, fmt.Errorf("不支持的容器类型: %s", manifest.Kind)
	}
	return manifest, plain[4+manifestLen:], mark, nil
}

// validateJunxManifest 校验清单中的偏移与长度，防止越界读取。
func validateJunxManifest(manifest junxManifest, body []byte) error {
	if manifest.Kind == junxFileKind && len(manifest.Entries) != 1 {
		return fmt.Errorf("文件容器应只包含一条记录")
	}
	for _, entry := range manifest.Entries {
		if entry.Dir {
			if entry.Size != 0 {
				return fmt.Errorf("目录记录不应包含内容长度: %s", entry.Path)
			}
			continue
		}
		if entry.Offset < 0 || entry.Size < 0 || entry.Offset+entry.Size > int64(len(body)) {
			return fmt.Errorf("文件记录越界: %s", entry.Path)
		}
	}
	return nil
}

// unpackJunxPayload 按清单把内容写回 destDir，重名时自动追加 (2)(3)。
func unpackJunxPayload(manifest junxManifest, body []byte, destDir string, progress junxProgressFunc) (string, error) {
	if err := validateJunxManifest(manifest, body); err != nil {
		return "", err
	}

	name := sanitizeFileName(manifest.Name)
	if name == "" {
		name = "恢复的文件"
	}
	isFolder := manifest.Kind == junxFolderKind
	target := uniqueDestPath(filepath.Join(destDir, name), isFolder)

	reportProgress(progress, "extract", 50, name)

	if !isFolder {
		entry := manifest.Entries[0]
		if err := writeJunxEntry(target, body, entry); err != nil {
			return "", err
		}
		reportProgress(progress, "extract", 95, name)
		return target, nil
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("创建目录失败 %s: %w", target, err)
	}

	total := len(manifest.Entries)
	for index, entry := range manifest.Entries {
		rel, err := safeRelPath(entry.Path)
		if err != nil {
			return "", err
		}
		full := filepath.Join(target, rel)
		if entry.Dir {
			if err := os.MkdirAll(full, 0o755); err != nil {
				return "", fmt.Errorf("创建目录失败 %s: %w", full, err)
			}
		} else {
			if err := writeJunxEntry(full, body, entry); err != nil {
				return "", err
			}
		}
		if total > 0 {
			reportProgress(progress, "extract", 50+index*45/total, rel)
		}
	}
	reportProgress(progress, "extract", 95, name)
	return target, nil
}

// writeJunxEntry 把 body 中对应区间写入目标文件。
func writeJunxEntry(full string, body []byte, entry junxEntry) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	content := body[entry.Offset : entry.Offset+entry.Size]
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("写入文件失败 %s: %w", full, err)
	}
	return nil
}

// safeRelPath 把 manifest 中的 / 相对路径转换为系统路径，并拒绝越界。
func safeRelPath(rel string) (string, error) {
	value := strings.TrimSuffix(filepath.ToSlash(rel), "/")
	if value == "" || value == "." {
		return "", fmt.Errorf("目录清单包含空路径")
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", fmt.Errorf("目录清单包含非法路径: %s", rel)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("目录清单包含非法路径: %s", rel)
		}
	}
	return filepath.FromSlash(value), nil
}

// junxOutputPath 计算 .junx 输出路径：默认与源同目录、同名为基础，自动补扩展名。
func junxOutputPath(src, outputName string) (string, error) {
	dir := filepath.Dir(src)
	name := strings.TrimSpace(outputName)
	if name == "" {
		name = filepath.Base(src)
	}
	name = sanitizeFileName(name)
	if name == "" {
		return "", fmt.Errorf("输出名称无效")
	}
	if !strings.HasSuffix(strings.ToLower(name), junxExt) {
		name += junxExt
	}
	return filepath.Join(dir, name), nil
}

// uniqueDestPath 在目标已存在时依次尝试 "名称 (2)"、"名称 (3)"。
func uniqueDestPath(path string, isDir bool) string {
	if !junxPathExists(path) {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	stem := base
	ext := ""
	if !isDir {
		ext = filepath.Ext(base)
		stem = strings.TrimSuffix(base, ext)
	}
	for index := 2; index <= 9999; index++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, index, ext))
		if !pathExists(candidate) {
			return candidate
		}
	}
	return path
}

func junxPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// sanitizeFileName 去除 Windows 不允许的字符，并处理保留名与结尾的句点空格。
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	var builder strings.Builder
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`\/:*?"<>|`, r) {
			builder.WriteRune('_')
			continue
		}
		builder.WriteRune(r)
	}
	cleaned := strings.TrimRight(builder.String(), " .")
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return ""
	}
	if isReservedWindowsName(cleaned) {
		return "_" + cleaned
	}
	return cleaned
}

// isReservedWindowsName 判断是否为 Windows 设备保留名（CON、NUL、COM1 等）。
func isReservedWindowsName(name string) bool {
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5",
		"COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4",
		"LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// reportProgress 在回调存在时上报一次进度。
func reportProgress(progress junxProgressFunc, phase string, percent int, current string) {
	if progress == nil {
		return
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	progress(phase, percent, current)
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// deleteOriginalText 是"永久删除源路径"原生复选框文案的三语版本。
type deleteOriginalText struct {
	Title    string
	Question string
	Yes      string
	No       string
}

func deleteOriginalTextFor(locale, name string, isFolder bool) deleteOriginalText {
	object := "文件"
	if isFolder {
		object = "文件夹"
	}
	switch normalizedLocale(locale) {
	case "en":
		target := "file"
		if isFolder {
			target = "folder"
		}
		return deleteOriginalText{
			Title:    "Permanently delete",
			Question: fmt.Sprintf("Encryption is complete. Permanently delete the original %s \"%s\"? This cannot be undone and skips the Recycle Bin.", target, name),
			Yes:      "Delete",
			No:       "Keep",
		}
	case "ja":
		target := "ファイル"
		if isFolder {
			target = "フォルダ"
		}
		return deleteOriginalText{
			Title:    "完全削除",
			Question: fmt.Sprintf("暗号化が完了しました。元の%s「%s」を完全に削除しますか？この操作は元に戻せず、ごみ箱にも入りません。", target, name),
			Yes:      "削除する",
			No:       "残す",
		}
	default:
		return deleteOriginalText{
			Title:    "永久删除",
			Question: fmt.Sprintf("加密已完成。是否永久删除原%s「%s」？此操作不可撤销，且不进入回收站。", object, name),
			Yes:      "删除",
			No:       "保留",
		}
	}
}
