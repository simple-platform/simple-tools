package deploy

import (
	"bytes"
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
	Client  HTTPClient
	Store   TokenStore
	Decoder JWTDecoder
	TimeNow func() time.Time
	cache   *TokenCache
	cacheMu sync.Mutex
}

// NewAuthenticator creates an Authenticator with default dependencies.
func NewAuthenticator() *Authenticator {
	return &Authenticator{
		Client:  &http.Client{Timeout: 30 * time.Second},
		Store:   &FileTokenStore{},
		Decoder: &DefaultJWTDecoder{},
		TimeNow: time.Now,
	}
}

// GetJWT returns a valid JWT for the environment, fetching if needed.
// Token expiry is extracted from the JWT's exp claim.
func (a *Authenticator) GetJWT(endpoint, apiKey, env string) (string, error) {
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
	cached, ok := a.cache.Tokens[env]
	a.cache.mu.RUnlock()

	// Check if cached token is still valid (with 5 min buffer before expiry)
	if ok && a.TimeNow().Add(5*time.Minute).Before(cached.ExpiresAt) {
		return cached.AccessToken, nil
	}

	// Exchange API key for JWT
	token, err := a.exchangeAPIKeyForJWT(endpoint, apiKey)
	if err != nil {
		return "", err
	}

	// Verify JWT Signature (Debugging step)
	if err := a.VerifyJWT(endpoint, token); err != nil {
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
	a.cache.Tokens[env] = CachedToken{
		AccessToken: token,
		ExpiresAt:   expiresAt,
	}
	a.cache.mu.Unlock()

	// Save cache (ignore errors - caching is best effort)
	_ = a.Store.Save(a.cache)

	return token, nil
}

// exchangeAPIKeyForJWT calls the identity endpoint to exchange API key for JWT.
func (a *Authenticator) exchangeAPIKeyForJWT(endpoint, apiKey string) (string, error) {
	url := fmt.Sprintf("https://%s/auth/login", endpoint)

	// Request body with api_key
	body := fmt.Sprintf(`{"api_key":"%s"}`, apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read auth response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("authentication failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse auth response: %w", err)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return result.AccessToken, nil
}

// VerifyJWT checks the JWT signature against the Identity service's JWKS.
func (a *Authenticator) VerifyJWT(endpoint, token string) error {
	// Fetch JWKS
	jwksURL := fmt.Sprintf("https://%s/.well-known/jwks.json", endpoint)
	req, err := http.NewRequest("GET", jwksURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("JWKS request failed: %w", err)
	}
	defer resp.Body.Close()

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

	if !VerifyEd25519(pubKeyBytes, message, signature) {
		return fmt.Errorf("invalid JWT signature")
	}

	return nil
}

// VerifyEd25519 verifies the signature using crypto/ed25519.
var VerifyEd25519 = func(publicKey []byte, message, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}

// ClearCache removes cached token for an environment.
func (a *Authenticator) ClearCache(env string) error {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	if a.cache == nil {
		return nil
	}

	a.cache.mu.Lock()
	delete(a.cache.Tokens, env)
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
