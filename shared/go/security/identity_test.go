package security

import (
	"path/filepath"
	"testing"
)

func TestIdentityEncryptDecrypt(t *testing.T) {
	identity, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.pem"))
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if identity.DeviceID("client") == "" {
		t.Fatal("device id should not be empty")
	}

	plaintext := []byte("sm2 protects the session key")
	ciphertext, err := EncryptForPublicKey(identity.PublicPEM, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := identity.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext mismatch: got %q want %q", got, plaintext)
	}
}
