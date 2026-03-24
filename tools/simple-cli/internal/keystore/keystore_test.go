package keystore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndSave(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	kp, err := GenerateAndSave("abc123")
	if err != nil {
		t.Fatalf("GenerateAndSave() error = %v", err)
	}
	if kp.IDSuffix != "abc123" {
		t.Errorf("IDSuffix = %q, want abc123", kp.IDSuffix)
	}
	if len(kp.PrivateKey) == 0 {
		t.Error("PrivateKey is empty")
	}
	if kp.PublicJWK["kty"] != "OKP" || kp.PublicJWK["crv"] != "Ed25519" || kp.PublicJWK["kid"] != "abc123" || kp.PublicJWK["x"] == "" {
		t.Errorf("unexpected JWK: %v", kp.PublicJWK)
	}

	// private.pem MUST be 0600 — world-readable private keys are a security failure
	info, err := os.Stat(filepath.Join(Dir(), "abc123", "private.pem"))
	if err != nil {
		t.Fatalf("private.pem not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("private.pem mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestIsEnrolled_FalseBeforeMark(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, _ = GenerateAndSave("def456")
	if IsEnrolled("def456") {
		t.Error("IsEnrolled() = true before MarkEnrolled, want false")
	}
}

func TestMarkEnrolled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, _ = GenerateAndSave("ghi789")
	if err := MarkEnrolled("ghi789"); err != nil {
		t.Fatalf("MarkEnrolled() error = %v", err)
	}
	if !IsEnrolled("ghi789") {
		t.Error("IsEnrolled() = false after MarkEnrolled, want true")
	}
}

func TestGenerateOrLoad_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	kp1, err := GenerateOrLoad("jkl012")
	if err != nil {
		t.Fatalf("first GenerateOrLoad() error = %v", err)
	}
	kp2, err := GenerateOrLoad("jkl012")
	if err != nil {
		t.Fatalf("second GenerateOrLoad() error = %v", err)
	}
	if string(kp1.PrivateKey) != string(kp2.PrivateKey) {
		t.Error("private keys differ between loads — keypair is not stable")
	}
}

func TestDeleteKey(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, _ = GenerateAndSave("mno345")
	_ = MarkEnrolled("mno345")

	if err := DeleteKey("mno345"); err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}
	if IsEnrolled("mno345") {
		t.Error("IsEnrolled() = true after DeleteKey, want false")
	}
	if _, err := os.Stat(filepath.Join(Dir(), "mno345")); !os.IsNotExist(err) {
		t.Error("key directory still exists after DeleteKey")
	}
}
