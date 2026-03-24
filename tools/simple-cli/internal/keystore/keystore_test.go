package keystore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndSave(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	kp, err := GenerateAndSave("acme", "dev", "abc123")
	if err != nil {
		t.Fatalf("GenerateAndSave() error = %v", err)
	}
	if kp.IDSuffix != "abc123" {
		t.Errorf("IDSuffix = %q, want abc123", kp.IDSuffix)
	}
	if len(kp.PrivateKey) == 0 {
		t.Error("PrivateKey is empty")
	}
	if kp.PublicJWK["kty"] != "OKP" || kp.PublicJWK["crv"] != "Ed25519" || kp.PublicJWK["kid"] != "KEYabc123" || kp.PublicJWK["x"] == "" {
		t.Errorf("unexpected JWK: %v", kp.PublicJWK)
	}

	// private.pem MUST be 0600 — world-readable private keys are a security failure
	info, err := os.Stat(filepath.Join(Dir(), "acme", "dev", "abc123", "private.pem"))
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

	_, _ = GenerateAndSave("acme", "dev", "def456")
	if IsEnrolled("acme", "dev", "def456") {
		t.Error("IsEnrolled() = true before MarkEnrolled, want false")
	}
}

func TestMarkEnrolled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, _ = GenerateAndSave("acme", "dev", "ghi789")
	if err := MarkEnrolled("acme", "dev", "ghi789"); err != nil {
		t.Fatalf("MarkEnrolled() error = %v", err)
	}
	if !IsEnrolled("acme", "dev", "ghi789") {
		t.Error("IsEnrolled() = false after MarkEnrolled, want true")
	}
}

func TestGenerateOrLoad_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	kp1, err := GenerateOrLoad("acme", "dev", "jkl012")
	if err != nil {
		t.Fatalf("first GenerateOrLoad() error = %v", err)
	}
	kp2, err := GenerateOrLoad("acme", "dev", "jkl012")
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

	_, _ = GenerateAndSave("acme", "dev", "mno345")
	_ = MarkEnrolled("acme", "dev", "mno345")

	if err := DeleteKey("acme", "dev", "mno345"); err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}
	if IsEnrolled("acme", "dev", "mno345") {
		t.Error("IsEnrolled() = true after DeleteKey, want false")
	}
	if _, err := os.Stat(filepath.Join(Dir(), "acme", "dev", "mno345")); !os.IsNotExist(err) {
		t.Error("key directory still exists after DeleteKey")
	}
}

func TestEnvIsolation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Generate and enroll for "dev"
	_, _ = GenerateAndSave("acme", "dev", "iso999")
	_ = MarkEnrolled("acme", "dev", "iso999")

	// The same key in "prod" must NOT appear enrolled
	_, _ = GenerateAndSave("acme", "prod", "iso999")
	if IsEnrolled("acme", "prod", "iso999") {
		t.Error("prod key incorrectly appears enrolled after only dev was enrolled")
	}

	// The two directories must be distinct paths
	devDir := filepath.Join(Dir(), "acme", "dev", "iso999")
	prodDir := filepath.Join(Dir(), "acme", "prod", "iso999")
	if devDir == prodDir {
		t.Error("dev and prod key directories are the same — env isolation is broken")
	}
}
