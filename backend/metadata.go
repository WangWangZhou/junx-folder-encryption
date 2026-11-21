package backend

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	// dsStoreName 是写入目标文件夹内部的元数据文件名。
	dsStoreName = ".DS_Store"
	// dsStoreMagic 用于识别本工具写入的元数据，避免误读 macOS 真实文件。
	dsStoreMagic = "FOLDERHIDE-D"
	// dsStoreVersion 是元数据格式版本。
	dsStoreVersion = 1
)

// dsStoreMeta 是 .DS_Store 中保存的加密信封。
type dsStoreMeta struct {
	Magic      string `json:"magic"`
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	HiddenAt   string `json:"hiddenAt"`
}

// metadataKey 由内置字符串派生 32 字节 AES 密钥。
func metadataKey() []byte {
	sum := sha256.Sum256([]byte("snipaste"))
	return sum[:]
}

// encryptPayload 将任意载荷封装为 AES-256-GCM 加密的 .DS_Store JSON 数据。
func encryptPayload(payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(metadataKey())
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
	ciphertext := gcm.Seal(nil, nonce, payload, nil)

	meta := dsStoreMeta{
		Magic:      dsStoreMagic,
		Version:    dsStoreVersion,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		HiddenAt:   time.Now().Format("2006-01-02 15:04:05"),
	}
	return json.MarshalIndent(meta, "", "  ")
}

// decryptPayload 校验信封并解出原始载荷。
func decryptPayload(data []byte) ([]byte, error) {
	var meta dsStoreMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf(".DS_Store 元数据格式损坏: %w", err)
	}
	if meta.Magic != dsStoreMagic {
		return nil, fmt.Errorf(".DS_Store 不是本工具写入的元数据")
	}
	if meta.Version != dsStoreVersion {
		return nil, fmt.Errorf(".DS_Store 元数据版本不支持: %d", meta.Version)
	}
	nonce, err := base64.StdEncoding.DecodeString(meta.Nonce)
	if err != nil {
		return nil, fmt.Errorf(".DS_Store nonce 无效: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(meta.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf(".DS_Store 密文无效: %w", err)
	}
	block, err := aes.NewCipher(metadataKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf(".DS_Store nonce 长度错误")
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf(".DS_Store 解密失败（元数据可能被篡改）: %w", err)
	}
	return plain, nil
}
