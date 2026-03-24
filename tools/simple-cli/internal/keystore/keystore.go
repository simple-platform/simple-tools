package keystore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrKeyAlreadyExists is returned natively when a cryptographic keypair tries to securely re-initialize itself concurrently.
var ErrKeyAlreadyExists = errors.New("key already generated securely")

// Keypair holds an Ed25519 key pair and the id_suffix that identifies it.
// Note: id_suffix is the bare identifier from the API key — the "KEY"
// DB prefix is never used here; it has no meaning outside the database.
type Keypair struct {
	IDSuffix   string
	PrivateKey ed25519.PrivateKey
	PublicJWK  map[string]string // {"kty":"OKP","crv":"Ed25519","x":"<base64url>"}
}

// Dir returns ~/.simple/keys — the root directory for all keypairs.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".simple", "keys")
}

// keyDir returns the directory for a specific tenant, env, and id_suffix.
// e.g. ~/.simple/keys/acme/prod/000001f097af4c/
func keyDir(tenant, env, idSuffix string) string {
	return filepath.Join(Dir(), tenant, env, idSuffix)
}

// IsEnrolled returns true when both private.pem and the .enrolled sentinel
// exist for the given tenant, env, and id_suffix.
func IsEnrolled(tenant, env, idSuffix string) bool {
	dir := keyDir(tenant, env, idSuffix)
	_, e1 := os.Stat(filepath.Join(dir, "private.pem"))
	_, e2 := os.Stat(filepath.Join(dir, ".enrolled"))
	return e1 == nil && e2 == nil
}

// GenerateAndSave creates a new Ed25519 keypair and persists it to disk under the tenant+env-scoped directory.
func GenerateAndSave(tenant, env, idSuffix string) (*Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("keygen failed: %w", err)
	}

	dir := keyDir(tenant, env, idSuffix)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key dir: %w", err)
	}

	// Attempt to create the private key file exclusively to prevent TOCTOU races
	privPath := filepath.Join(dir, "private.pem")
	f, err := os.OpenFile(privPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%w", ErrKeyAlreadyExists)
		}
		return nil, fmt.Errorf("failed to open private.pem exclusively: %w", err)
	}
	defer func() { _ = f.Close() }()

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PKCS8: %w", err)
	}

	privBlock := &pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes}
	if _, err := f.Write(pem.EncodeToMemory(privBlock)); err != nil {
		return nil, fmt.Errorf("failed to write private.pem: %w", err)
	}

	jwk := buildPublicJWK(pub, idSuffix)
	jwkBytes, _ := json.Marshal(jwk)
	if err := os.WriteFile(filepath.Join(dir, "public.jwk"), jwkBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to write public.jwk: %w", err)
	}

	return &Keypair{IDSuffix: idSuffix, PrivateKey: priv, PublicJWK: jwk}, nil
}

// Load reads an existing keypair from disk for the given tenant and env. Does not check enrollment status.
func Load(tenant, env, idSuffix string) (*Keypair, error) {
	dir := keyDir(tenant, env, idSuffix)
	privPath := filepath.Join(dir, "private.pem")
	data, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("private key not found for %s: %w", idSuffix, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM for %s", idSuffix)
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 for %s: %w", idSuffix, err)
	}
	priv, ok := privKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unexpected key type for %s", idSuffix)
	}

	jwkData, err := os.ReadFile(filepath.Join(dir, "public.jwk"))
	if err != nil {
		return nil, fmt.Errorf("public.jwk not found for %s: %w", idSuffix, err)
	}
	var jwk map[string]string
	if err := json.Unmarshal(jwkData, &jwk); err != nil {
		return nil, fmt.Errorf("corrupt public.jwk for %s: %w", idSuffix, err)
	}

	return &Keypair{IDSuffix: idSuffix, PrivateKey: priv, PublicJWK: jwk}, nil
}

// GenerateOrLoad returns the keypair for the given tenant, env, and id_suffix,
// generating a new one if it doesn't exist yet.
func GenerateOrLoad(tenant, env, idSuffix string) (*Keypair, error) {
	// Optimistic load
	if kp, err := Load(tenant, env, idSuffix); err == nil {
		return kp, nil
	}
	// Try generation (handles O_EXCL race natively returning an error if created underneath us)
	kp, err := GenerateAndSave(tenant, env, idSuffix)
	if errors.Is(err, ErrKeyAlreadyExists) {
		return Load(tenant, env, idSuffix)
	}
	return kp, err
}

// MarkEnrolled writes the .enrolled sentinel file after a successful
// POST /auth/api-key/enroll for the given tenant and env.
func MarkEnrolled(tenant, env, idSuffix string) error {
	return os.WriteFile(filepath.Join(keyDir(tenant, env, idSuffix), ".enrolled"), []byte{}, 0600)
}

// DeleteKey removes the entire env-scoped keypair directory.
func DeleteKey(tenant, env, idSuffix string) error {
	return os.RemoveAll(keyDir(tenant, env, idSuffix))
}

// buildPublicJWK constructs the minimal OKP JWK for an Ed25519 public key.
func buildPublicJWK(pub ed25519.PublicKey, idSuffix string) map[string]string {
	return map[string]string{
		"kty": "OKP",
		"crv": "Ed25519",
		"kid": "KEY" + idSuffix,
		"x":   base64.RawURLEncoding.EncodeToString(pub),
	}
}
