package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sealForTest 走完整的打包 + 加密流程，返回容器字节。
func sealForTest(t *testing.T, src, kind string) []byte {
	t.Helper()
	payload, err := buildJunxPayload(src, kind, nil)
	if err != nil {
		t.Fatalf("buildJunxPayload: %v", err)
	}
	plain, err := packJunxPayload(payload.manifest, payload.body)
	if err != nil {
		t.Fatalf("packJunxPayload: %v", err)
	}
	sealed, err := sealJunxData(plain, markForKind(payload.manifest.Kind))
	if err != nil {
		t.Fatalf("sealJunxData: %v", err)
	}
	return sealed
}

// unpackFromBytes 解密容器并解包到 destDir。
func unpackFromBytes(t *testing.T, data []byte, destDir string) (string, error) {
	t.Helper()
	manifest, body, _, err := openJunxData(data)
	if err != nil {
		return "", err
	}
	return unpackJunxPayload(manifest, body, destDir, nil)
}

func TestJunxFileRoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "报告 2026.txt")
	content := "第一行\n第二行\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sealed := sealForTest(t, src, junxFileKind)
	if strings.Contains(string(sealed), "报告") {
		t.Fatalf("容器内不应出现明文文件名")
	}

	destDir := t.TempDir()
	target, err := unpackFromBytes(t, sealed, destDir)
	if err != nil {
		t.Fatalf("解包失败: %v", err)
	}
	if filepath.Base(target) != "报告 2026.txt" {
		t.Fatalf("文件名不一致: %s", filepath.Base(target))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("内容不一致: %q", string(got))
	}
}

func TestJunxFolderRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "私人资料")
	mustMkdir(t, filepath.Join(root, "子目录", "更深的目录"))
	mustMkdir(t, filepath.Join(root, "空目录"))
	mustWrite(t, filepath.Join(root, "a.txt"), "AAA")
	mustWrite(t, filepath.Join(root, "子目录", "照片.jpg"), strings.Repeat("x", 5000))
	mustWrite(t, filepath.Join(root, "子目录", "更深的目录", "笔记.md"), "笔记内容")

	sealed := sealForTest(t, root, junxFolderKind)
	if strings.Contains(string(sealed), "子目录") {
		t.Fatalf("容器内不应出现明文目录名")
	}

	destDir := t.TempDir()
	target, err := unpackFromBytes(t, sealed, destDir)
	if err != nil {
		t.Fatalf("解包失败: %v", err)
	}
	if filepath.Base(target) != "私人资料" {
		t.Fatalf("根目录名不一致: %s", filepath.Base(target))
	}
	if _, err := os.Stat(filepath.Join(target, "空目录")); err != nil {
		t.Fatalf("空目录未恢复: %v", err)
	}
	check := filepath.Join(target, "子目录", "更深的目录", "笔记.md")
	got, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("读取恢复文件失败: %v", err)
	}
	if string(got) != "笔记内容" {
		t.Fatalf("内容不一致: %q", string(got))
	}
	if info, err := os.Stat(filepath.Join(target, "子目录", "照片.jpg")); err != nil {
		t.Fatal(err)
	} else if info.Size() != 5000 {
		t.Fatalf("文件长度不一致: %d", info.Size())
	}
}

func TestJunxDecryptAutoRename(t *testing.T) {
	// 文件夹重名：私人资料 → 私人资料 (2)
	root := filepath.Join(t.TempDir(), "私人资料")
	mustMkdir(t, root)
	mustWrite(t, filepath.Join(root, "a.txt"), "A")
	folderData := sealForTest(t, root, junxFolderKind)

	destDir := t.TempDir()
	first, err := unpackFromBytes(t, folderData, destDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := unpackFromBytes(t, folderData, destDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "私人资料" || filepath.Base(second) != "私人资料 (2)" {
		t.Fatalf("文件夹重名处理错误: %s / %s", filepath.Base(first), filepath.Base(second))
	}

	// 文件重名：报告.txt → 报告 (2).txt，且保留扩展名
	fileSrcDir := t.TempDir()
	fileSrc := filepath.Join(fileSrcDir, "报告.txt")
	mustWrite(t, fileSrc, "B")
	fileData := sealForTest(t, fileSrc, junxFileKind)

	fileDest := t.TempDir()
	firstFile, err := unpackFromBytes(t, fileData, fileDest)
	if err != nil {
		t.Fatal(err)
	}
	secondFile, err := unpackFromBytes(t, fileData, fileDest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(firstFile) != "报告.txt" || filepath.Base(secondFile) != "报告 (2).txt" {
		t.Fatalf("文件重名处理错误: %s / %s", filepath.Base(firstFile), filepath.Base(secondFile))
	}
}

func TestJunxTamperRejected(t *testing.T) {
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "a.txt")
	mustWrite(t, src, "content")
	sealed := sealForTest(t, src, junxFileKind)

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, _, _, err := openJunxData(tampered); err == nil {
		t.Fatalf("篡改后的容器应解密失败")
	}

	badMagic := append([]byte(nil), sealed...)
	copy(badMagic[:8], "XXXXXXXX")
	if _, _, _, err := openJunxData(badMagic); err == nil {
		t.Fatalf("错误的文件头应被拒绝")
	}

	if _, _, _, err := openJunxData(sealed[:10]); err == nil {
		t.Fatalf("过短的输入应被拒绝")
	}
}

func TestJunxOutputPath(t *testing.T) {
	src := filepath.Join(t.TempDir(), "报告.docx")

	got, err := junxOutputPath(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "报告.docx.junx" {
		t.Fatalf("默认输出名错误: %s", filepath.Base(got))
	}

	got, err = junxOutputPath(src, " 备份 ")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "备份.junx" {
		t.Fatalf("自定义输出名错误: %s", filepath.Base(got))
	}

	got, err = junxOutputPath(src, "备份.JUNX")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "备份.JUNX" {
		t.Fatalf("已有扩展名不应重复追加: %s", filepath.Base(got))
	}

	if _, err := junxOutputPath(src, "a/b"); err != nil {
		t.Fatalf("路径分隔符应被清洗而不是报错: %v", err)
	}
}

func TestJunxSafeRelPath(t *testing.T) {
	if _, err := safeRelPath("../evil.txt"); err == nil {
		t.Fatalf("应拒绝 .. 路径")
	}
	if _, err := safeRelPath("子目录/../../evil.txt"); err == nil {
		t.Fatalf("应拒绝跨级路径")
	}
	if _, err := safeRelPath("/abs.txt"); err == nil {
		t.Fatalf("应拒绝绝对路径")
	}
	if _, err := safeRelPath("C:/evil.txt"); err == nil {
		t.Fatalf("应拒绝盘符路径")
	}
	got, err := safeRelPath("子目录/a.txt")
	if err != nil {
		t.Fatalf("正常路径不应报错: %v", err)
	}
	if got != filepath.Join("子目录", "a.txt") {
		t.Fatalf("路径转换错误: %s", got)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"a:b.txt":   "a_b.txt",
		"a/b":       "a_b",
		"name.":     "name",
		"CON":       "_CON",
		"  report ": "report",
		"正常名称.txt":  "正常名称.txt",
	}
	for input, want := range cases {
		if got := sanitizeFileName(input); got != want {
			t.Fatalf("sanitizeFileName(%q) = %q，期望 %q", input, got, want)
		}
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTestService 构造已解锁、PIN 配置落在临时目录的服务实例。
func newTestService(t *testing.T) *FolderService {
	t.Helper()
	store := newPinStore(filepath.Join(t.TempDir(), "pin.json"))
	return &FolderService{logs: []string{}, pins: store, unlocked: true}
}

func TestServiceJunxFileFlow(t *testing.T) {
	service := newTestService(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "note.txt")
	mustWrite(t, src, "hello")

	if err := service.EncryptFileToJunx(src, "", false, "zh-CN"); err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	junx := filepath.Join(dir, "note.txt.junx")
	if _, err := os.Stat(junx); err != nil {
		t.Fatalf("未生成容器: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("未勾选删除时原文件应保留: %v", err)
	}

	if err := service.DecryptJunx(junx); err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	restored := filepath.Join(dir, "note (2).txt")
	got, err := os.ReadFile(restored)
	if err != nil {
		t.Fatalf("未按重名规则输出: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("内容不一致: %q", got)
	}
	if len(service.RecentLogs()) == 0 {
		t.Fatalf("应有操作日志")
	}
}

func TestServiceJunxGenericDecrypt(t *testing.T) {
	service := newTestService(t)
	root := filepath.Join(t.TempDir(), "资料")
	mustMkdir(t, root)
	mustWrite(t, filepath.Join(root, "a.txt"), "A")
	if err := service.EncryptFolderToJunx(root, "归档", false, "zh-CN"); err != nil {
		t.Fatalf("文件夹加密失败: %v", err)
	}

	junx := filepath.Join(filepath.Dir(root), "归档.junx")
	// 通用解密：不需要知道容器里是文件还是文件夹。
	if err := service.DecryptJunx(junx); err != nil {
		t.Fatalf("通用解密失败: %v", err)
	}
	// 容器名只影响容器本身，解密恢复的是清单里的原名；原目录仍在，故加 (2)。
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "资料 (2)")); err != nil {
		t.Fatalf("未恢复到容器所在目录: %v", err)
	}
}

func TestServiceJunxOutputExistsRejected(t *testing.T) {
	service := newTestService(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	mustWrite(t, src, "A")
	if err := os.WriteFile(filepath.Join(dir, "a.txt.junx"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.EncryptFileToJunx(src, "", false, "zh-CN"); err == nil {
		t.Fatalf("目标已存在时应拒绝覆盖")
	}
}

func TestProbePaths(t *testing.T) {
	service := newTestService(t)
	dir := t.TempDir()

	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "A")
	folder := filepath.Join(dir, "资料")
	mustMkdir(t, folder)

	junxSrc := filepath.Join(dir, "b.txt")
	mustWrite(t, junxSrc, "B")
	if err := service.EncryptFileToJunx(junxSrc, "", false, "zh-CN"); err != nil {
		t.Fatalf("准备容器失败: %v", err)
	}
	junx := filepath.Join(dir, "b.txt.junx")

	missing := filepath.Join(dir, "missing.txt")
	probes := service.ProbePaths([]string{file, folder, junx, missing})
	if len(probes) != 4 {
		t.Fatalf("探测数量不对: %d", len(probes))
	}
	if probes[0].Kind != "file" || probes[0].OutputPath != file+junxExt || probes[0].OutputExists {
		t.Fatalf("普通文件探测错误: %+v", probes[0])
	}
	if probes[1].Kind != "folder" || probes[1].OutputPath != folder+junxExt {
		t.Fatalf("文件夹探测错误: %+v", probes[1])
	}
	if probes[2].Kind != "junx" || probes[2].ContainerKind != "file" {
		t.Fatalf("容器探测错误: %+v", probes[2])
	}
	if probes[3].Exists || probes[3].Kind != "missing" {
		t.Fatalf("不存在的路径探测错误: %+v", probes[3])
	}

	// 目标已存在时应标记 OutputExists，供前端做同名拦截。
	mustWrite(t, file+junxExt, "occupied")
	again := service.ProbePaths([]string{file})
	if !again[0].OutputExists {
		t.Fatalf("同名产物未被识别: %+v", again[0])
	}
}

func TestJunxKindMark(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWrite(t, file, "A")
	if data := sealForTest(t, file, junxFileKind); data[8] != junxMarkFile {
		t.Fatalf("文件容器标记错误: %#x", data[8])
	}

	root := filepath.Join(dir, "资料")
	mustMkdir(t, root)
	mustWrite(t, filepath.Join(root, "b.txt"), "B")
	if data := sealForTest(t, root, junxFolderKind); data[8] != junxMarkFolder {
		t.Fatalf("文件夹容器标记错误: %#x", data[8])
	}

	// 标记被篡改时仍以加密清单为准。
	payload, err := buildJunxPayload(root, junxFolderKind, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := packJunxPayload(payload.manifest, payload.body)
	if err != nil {
		t.Fatal(err)
	}
	mismatched, err := sealJunxData(plain, junxMarkFile)
	if err != nil {
		t.Fatal(err)
	}
	junxPath := filepath.Join(dir, "mismatch.junx")
	if err := os.WriteFile(junxPath, mismatched, 0o600); err != nil {
		t.Fatal(err)
	}

	service := newTestService(t)
	if err := service.DecryptJunx(junxPath); err != nil {
		t.Fatalf("标记不一致时仍应按清单还原: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "资料 (2)")); err != nil {
		t.Fatalf("未按清单重建目录: %v", err)
	}
}
