//go:build windows

package backend

import (
	"encoding/json"
	"fmt"

	"golang.org/x/sys/windows"
)

// aclBackup 保存加密写入 .DS_Store 的原始权限信息。
type aclBackup struct {
	SDDL      string `json:"sddl"`
	Protected bool   `json:"protected"`
}

// securityInfoFlags 根据原 DACL 是否受保护生成写回标志。
func securityInfoFlags(protected bool) windows.SECURITY_INFORMATION {
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	if protected {
		flags |= windows.SECURITY_INFORMATION(windows.PROTECTED_DACL_SECURITY_INFORMATION)
	} else {
		flags |= windows.SECURITY_INFORMATION(windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	}
	return flags
}

// currentUserSID 返回当前进程令牌所属用户的 SID。
func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("读取当前用户 SID 失败: %w", err)
	}
	if user == nil || user.User.Sid == nil {
		return nil, fmt.Errorf("当前用户 SID 为空")
	}
	return user.User.Sid, nil
}

// daclWithListDeny 在原安全描述符上追加“拒绝列目录”的 ACE。
// 只拒绝 FILE_LIST_DIRECTORY，保留 FILE_TRAVERSE，方便工具读取内部元数据。
func daclWithListDeny(original *windows.SECURITY_DESCRIPTOR) (*windows.ACL, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	entry := windows.EXPLICIT_ACCESS{
		AccessPermissions: windows.FILE_LIST_DIRECTORY,
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
	updated, err := windows.BuildSecurityDescriptor(nil, nil, []windows.EXPLICIT_ACCESS{entry}, nil, original)
	if err != nil {
		return nil, fmt.Errorf("构造拒绝列目录的 ACL 失败: %w", err)
	}
	dacl, _, err := updated.DACL()
	if err != nil {
		return nil, fmt.Errorf("读取新 DACL 失败: %w", err)
	}
	if dacl == nil {
		return nil, fmt.Errorf("新 DACL 为空")
	}
	return dacl, nil
}

// restoreACL 用备份的 SDDL 恢复目录 DACL。
func restoreACL(path string, backup aclBackup) error {
	sd, err := windows.SecurityDescriptorFromString(backup.SDDL)
	if err != nil {
		return fmt.Errorf("解析备份的权限信息失败: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("读取备份 DACL 失败: %w", err)
	}
	if dacl == nil {
		return fmt.Errorf("备份 DACL 为空")
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, securityInfoFlags(backup.Protected), nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("恢复目录权限失败: %w", err)
	}
	return nil
}

// applyListDeny 给目录追加“拒绝当前用户列目录”的 ACE。
func applyListDeny(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("读取目录权限失败: %w", err)
	}
	if sd == nil {
		return fmt.Errorf("目录安全描述符为空")
	}
	control, _, err := sd.Control()
	if err != nil {
		return fmt.Errorf("读取安全描述符控制位失败: %w", err)
	}
	protected := control&windows.SE_DACL_PROTECTED != 0
	dacl, err := daclWithListDeny(sd)
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, securityInfoFlags(protected), nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("应用拒绝列目录权限失败: %w", err)
	}
	return nil
}

// hideWithACL 备份 DACL，写入加密元数据，再拒绝当前用户列目录并设置隐藏属性。
func hideWithACL(path string) error {
	clean, err := canonicalPath(path)
	if err != nil {
		return err
	}
	if !isDirectoryRaw(clean) {
		return fmt.Errorf("目标不是有效文件夹: %s", clean)
	}
	if pathExists(joinChildPath(clean, dsStoreName)) {
		return fmt.Errorf("%s 已存在，拒绝覆盖", dsStoreName)
	}

	sd, err := windows.GetNamedSecurityInfo(clean, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("读取目录权限失败: %w", err)
	}
	if sd == nil {
		return fmt.Errorf("目录安全描述符为空")
	}
	sddl := sd.String()
	if sddl == "" {
		return fmt.Errorf("导出目录权限失败")
	}
	control, _, err := sd.Control()
	if err != nil {
		return fmt.Errorf("读取安全描述符控制位失败: %w", err)
	}
	backup := aclBackup{
		SDDL:      sddl,
		Protected: control&windows.SE_DACL_PROTECTED != 0,
	}
	payload, err := json.Marshal(backup)
	if err != nil {
		return err
	}
	encrypted, err := encryptPayload(payload)
	if err != nil {
		return err
	}
	if err := writeDSStore(clean, encrypted); err != nil {
		return err
	}

	if err := applyListDeny(clean); err != nil {
		_ = removeDSStore(clean)
		return err
	}
	if err := setFolderHidden(clean, true); err != nil {
		_ = restoreACL(clean, backup)
		_ = removeDSStore(clean)
		return err
	}
	return nil
}

// showWithACL 读取加密元数据，恢复 DACL，清除隐藏属性并删除元数据。
func showWithACL(path string) (string, error) {
	clean, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	if !isDirectoryRaw(clean) {
		return "", fmt.Errorf("目标不是有效文件夹: %s", clean)
	}

	data, err := readDSStore(clean)
	if err != nil {
		return "", err
	}
	payload, err := decryptPayload(data)
	if err != nil {
		return "", err
	}
	var backup aclBackup
	if err := json.Unmarshal(payload, &backup); err != nil {
		return "", fmt.Errorf(".DS_Store 权限备份格式损坏: %w", err)
	}
	if backup.SDDL == "" {
		return "", fmt.Errorf(".DS_Store 中的权限备份为空")
	}
	if err := restoreACL(clean, backup); err != nil {
		return "", err
	}
	if err := setFolderHidden(clean, false); err != nil {
		return "", err
	}

	warning := ""
	if err := removeDSStore(clean); err != nil {
		warning = fmt.Sprintf("目录权限已恢复，但删除 %s 失败: %v", dsStoreName, err)
	}
	return warning, nil
}
