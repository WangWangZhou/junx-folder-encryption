package backend

import "testing"

func TestEncryptDecryptPayload(t *testing.T) {
	payload := []byte(`{"sddl":"D:PAI(A;;FA;;;SY)","protected":true}`)
	data, err := encryptPayload(payload)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	plain, err := decryptPayload(data)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if string(plain) != string(payload) {
		t.Fatalf("载荷不一致: %s", plain)
	}
}

func TestDecryptRejectsBrokenMetadata(t *testing.T) {
	if _, err := decryptPayload([]byte("not json")); err == nil {
		t.Fatal("损坏的元数据应当失败")
	}
}
