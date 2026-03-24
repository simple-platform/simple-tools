package deploy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"simple-cli/internal/keystore"
)

// TokenCache stores JWT tokens per environment with expiry.
type TokenCache struct {
	Tokens map[string]CachedToken `json:"tokens"`
	mu     sync.RWMutex
}

// CachedToken represents a cached JWT with expiry.
type CachedToken struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// HTTPClient abstracts HTTP requests for testing.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenStore abstracts token persistence for testing.
type TokenStore interface {
	Load() (*TokenCache, error)
	Save(cache *TokenCache) error
}

// JWTDecoder abstracts JWT decoding for testing.
type JWTDecoder interface {
	DecodeExpiry(token string) (time.Time, error)
}

// Authenticator handles API key to JWT exchange and caching.
type Authenticator struct {
	Client          HTTPClient
	Store           TokenStore
	Decoder         JWTDecoder
	TimeNow         func() time.Time
	cache           *TokenCache
	cacheMu         sync.Mutex
	VerifySignature func(pub, msg, sig []byte) bool
}

// NewAuthenticator creates an Authenticator with default dependencies.
func NewAuthenticator() *Authenticator {
	return &Authenticator{
		Client:          &http.Client{Timeout: 30 * time.Second},
		Store:           &FileTokenStore{},
		Decoder:         &DefaultJWTDecoder{},
		TimeNow:         time.Now,
		VerifySignature: VerifyEd25519,
	}
}

// GetJWT returns a valid session JWT, fetching a new one if the cache is
// empty or within 5 minutes of expiry.
//
// tenantEnvKey must be in the form "tenant::env" (e.g. "acme::dev").
// This scopes the cache correctly so two tenants with the same env name
// (e.g. "acme::dev" and "contoso::dev") do not collide on the same machine.
func (a *Authenticator) GetJWT(ctx context.Context, endpoint, apiKey, tenantEnvKey string) (string, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	// Load cache if not loaded
	if a.cache == nil {
		cache, err := a.Store.Load()
		if err != nil {
			// If load fails, start fresh
			cache = &TokenCache{Tokens: make(map[string]CachedToken)}
		}
		a.cache = cache
	}

	a.cache.mu.RLock()
	cached, ok := a.cache.Tokens[tenantEnvKey]
	a.cache.mu.RUnlock()

	// Check if cached token is still valid (with 5 min buffer before expiry)
	if ok && a.TimeNow().Add(5*time.Minute).Before(cached.ExpiresAt) {
		return cached.AccessToken, nil
	}

	// Exchange API key for JWT
	parts := strings.SplitN(tenantEnvKey, "::", 2)
	tenant := parts[0]
	env := ""
	if len(parts) == 2 {
		env = parts[1]
	}
	token, err := a.enrollAndAuthenticate(ctx, endpoint, apiKey, tenant, env)
	if err != nil {
		return "", err
	}

	// Verify JWT Signature (Debugging step)
	if err := a.VerifyJWT(ctx, endpoint, token); err != nil {
		return "", fmt.Errorf("JWT verification failed: %w", err)
	}

	// Extract expiry from JWT's exp claim
	expiresAt, err := a.Decoder.DecodeExpiry(token)
	if err != nil {
		// If we can't decode expiry, use a safe default of 55 minutes
		expiresAt = a.TimeNow().Add(55 * time.Minute)
	}

	// Cache the token
	a.cache.mu.Lock()
	a.cache.Tokens[tenantEnvKey] = CachedToken{
		AccessToken: token,
		ExpiresAt:   expiresAt,
	}
	a.cache.mu.Unlock()

	// Save cache (ignore errors - caching is best effort)
	_ = a.Store.Save(a.cache)

	return token, nil
}

// enrollAndAuthenticate replaces exchangeAPIKeyForJWT.
// It handles one-time machine enrollment and then signs a fresh PoP JWT
// on every call to obtain a session JWT from the Identity Service.
func (a *Authenticator) enrollAndAuthenticate(ctx context.Context, endpoint, rawAPIKey, tenant, env string) (string, error) {
	idSuffix, err := ParseIDSuffix(rawAPIKey)
	if err != nil {
		return "", fmt.Errorf("invalid api key format: %w", err)
	}

	kp, err := keystore.GenerateOrLoad(tenant, env, idSuffix)
	if err != nil {
		return "", fmt.Errorf("keypair error for %s: %w", idSuffix, err)
	}

	// Enrollment is idempotent — skip if .enrolled sentinel exists for this env.
	if !keystore.IsEnrolled(tenant, env, idSuffix) {
		var enrollErr error
		// 2-attempt retry for network resilience
		for i := 0; i < 2; i++ {
			enrollErr = a.enrollKey(ctx, endpoint, rawAPIKey, kp.PublicJWK)
			if enrollErr == nil {
				break
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("enrollment canceled: %w", ctx.Err())
			case <-time.After(time.Duration(i+1) * 500 * time.Millisecond):
			}
		}
		if enrollErr != nil {
			return "", fmt.Errorf("enrollment failed after retries: %w", enrollErr)
		}
		if err := keystore.MarkEnrolled(tenant, env, idSuffix); err != nil {
			return "", fmt.Errorf("failed to mark enrollment: %w", err)
		}
	}

	// Sign a fresh PoP JWT — never cached, expires in 60 seconds.
	popJWT, err := SignPopJWT(kp.PrivateKey, idSuffix)
	if err != nil {
		return "", fmt.Errorf("failed to sign PoP JWT: %w", err)
	}

	// Server expects: "si_<id_suffix>.<compact_signed_jwt>"
	return a.loginWithPoP(ctx, endpoint, "si_"+idSuffix+"."+popJWT)
}

// enrollKey calls POST /auth/api-key/enroll to register this machine's public key.
// This is called at most once per API key per machine.
func (a *Authenticator) enrollKey(ctx context.Context, endpoint, rawAPIKey string, publicJWK map[string]string) error {
	url := fmt.Sprintf("https://%s/auth/api-key/enroll", endpoint)
	body, err := json.Marshal(map[string]interface{}{
		"api_key":    rawAPIKey,
		"public_key": publicJWK,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal enroll request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create enroll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("enroll request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read enroll response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("enrollment rejected (%d): %s", resp.StatusCode, respBody)
	}
	return nil
}

// loginWithPoP calls POST /auth/login with the composed PoP auth string.
func (a *Authenticator) loginWithPoP(ctx context.Context, endpoint, authString string) (string, error) {
	url := fmt.Sprintf("https://%s/auth/login", endpoint)
	body, err := json.Marshal(map[string]string{"api_key": authString})
	if err != nil {
		return "", fmt.Errorf("failed to marshal login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read login response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authentication failed (%d): %s", resp.StatusCode, respBody)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("invalid auth response: %s", respBody)
	}

	return result.AccessToken, nil
}

// TenantEnvKey standardizes the cache key format isolating multiple tenants across the single cache map securely.
func TenantEnvKey(tenant, env string) string {
	return fmt.Sprintf("%s::%s", tenant, env)
}

// VerifyJWT checks the JWT signature against the Identity service's JWKS.
func (a *Authenticator) VerifyJWT(ctx context.Context, endpoint, token string) error {
	// Fetch JWKS
	jwksURL := fmt.Sprintf("https://%s/.well-known/jwks.json", endpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("JWKS request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch JWKS from %s: status %d", jwksURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read JWKS response: %w", err)
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Crv string `json:"crv"`
			X   string `json:"x"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("failed to parse JWKS: %w", err)
	}

	// Parse Token Header to find kid
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT format")
	}

	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return fmt.Errorf("failed to decode JWT header: %w", err)
	}

	var header struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("failed to parse JWT header: %w", err)
	}

	if header.Alg != "EdDSA" {
		return fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// Find matching key
	var pubKeyBytes []byte
	for _, key := range jwks.Keys {
		if key.Kid == header.Kid && key.Kty == "OKP" && key.Crv == "Ed25519" {
			pubKeyBytes, err = base64URLDecode(key.X)
			if err != nil {
				return fmt.Errorf("invalid public key in JWKS: %w", err)
			}
			break
		}
	}

	if pubKeyBytes == nil {
		return fmt.Errorf("matching public key not found in JWKS for kid %s", header.Kid)
	}

	// Verify Signature
	message := []byte(parts[0] + "." + parts[1])
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return fmt.Errorf("failed to decode JWT signature: %w", err)
	}

	if len(pubKeyBytes) != 32 {
		return fmt.Errorf("invalid Ed25519 public key length: %d", len(pubKeyBytes))
	}

	if a.VerifySignature != nil && !a.VerifySignature(pubKeyBytes, message, signature) {
		return fmt.Errorf("invalid JWT signature")
	}

	return nil
}

// VerifyEd25519 verifies the signature using crypto/ed25519.
var VerifyEd25519 = func(publicKey []byte, message, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}

// ClearCache removes the cached session JWT for the given tenantEnvKey.
// tenantEnvKey format: "tenant::env" (e.g. "acme::dev")
func (a *Authenticator) ClearCache(tenantEnvKey string) error {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	if a.cache == nil {
		cache, err := a.Store.Load()
		if err != nil {
			cache = &TokenCache{Tokens: make(map[string]CachedToken)}
		}
		a.cache = cache
	}

	a.cache.mu.Lock()
	delete(a.cache.Tokens, tenantEnvKey)
	a.cache.mu.Unlock()

	return a.Store.Save(a.cache)
}

// DefaultJWTDecoder decodes JWT to extract expiry from exp claim.
type DefaultJWTDecoder struct{}

// DecodeExpiry extracts the exp claim from a JWT without verifying signature.
// JWT format: header.payload.signature (base64url encoded)
func (d *DefaultJWTDecoder) DecodeExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode the payload (second part)
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	// Parse the claims
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("exp claim not found in JWT")
	}

	return time.Unix(claims.Exp, 0), nil
}

// base64URLDecode decodes a base64url encoded string (JWT uses base64url, not base64).
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

// FileTokenStore implements TokenStore using filesystem.
type FileTokenStore struct {
	// ConfigDir overrides the default config directory for testing.
	ConfigDir string
}

// tokenCachePath returns the path to the token cache file.
func (s *FileTokenStore) tokenCachePath() string {
	dir := s.ConfigDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Fallback to current directory
			home = "."
		}
		dir = filepath.Join(home, ".simple")
	}
	return filepath.Join(dir, "tokens.json")
}

// Load reads the token cache from disk.
func (s *FileTokenStore) Load() (*TokenCache, error) {
	path := s.tokenCachePath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TokenCache{Tokens: make(map[string]CachedToken)}, nil
		}
		return nil, fmt.Errorf("failed to read token cache: %w", err)
	}

	var cache TokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		// If corrupted, start fresh
		return &TokenCache{Tokens: make(map[string]CachedToken)}, nil
	}

	if cache.Tokens == nil {
		cache.Tokens = make(map[string]CachedToken)
	}

	return &cache, nil
}

// Save writes the token cache to disk.
func (s *FileTokenStore) Save(cache *TokenCache) error {
	path := s.tokenCachePath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	cache.mu.RLock()
	data, err := json.MarshalIndent(cache, "", "  ")
	cache.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal token cache: %w", err)
	}

	// Write with restrictive permissions (only user can read/write)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write token cache: %w", err)
	}

	return nil
}
