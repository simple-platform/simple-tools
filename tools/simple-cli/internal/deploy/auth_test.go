package deploy

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// MockHTTPClient implements HTTPClient for testing.
type MockHTTPClient struct {
	Response *http.Response
	Err      error
	Requests []*http.Request
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.Requests = append(m.Requests, req)
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Response, nil
}

// MockTokenStore implements TokenStore for testing.
type MockTokenStore struct {
	Cache     *TokenCache
	LoadErr   error
	SaveErr   error
	SaveCalls int
}

func (m *MockTokenStore) Load() (*TokenCache, error) {
	if m.LoadErr != nil {
		return nil, m.LoadErr
	}
	if m.Cache == nil {
		return &TokenCache{Tokens: make(map[string]CachedToken)}, nil
	}
	return m.Cache, nil
}

func (m *MockTokenStore) Save(cache *TokenCache) error {
	m.SaveCalls++
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.Cache = cache
	return nil
}

// MockJWTDecoder implements JWTDecoder for testing.
type MockJWTDecoder struct {
	ExpiresAt time.Time
	Err       error
}

func (m *MockJWTDecoder) DecodeExpiry(token string) (time.Time, error) {
	if m.Err != nil {
		return time.Time{}, m.Err
	}
	return m.ExpiresAt, nil
}

// createTestJWT creates a valid JWT structure for testing.
func createTestJWT(exp int64) string {
	header := base64.URLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"at+jwt"}`))
	payload := base64.URLEncoding.EncodeToString([]byte(`{"exp":` + itoa(exp) + `,"sub":"test"}`))
	signature := base64.URLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + signature
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var result []byte
	for n > 0 {
		result = append([]byte{byte('0' + n%10)}, result...)
		n /= 10
	}
	return string(result)
}

func TestAuthenticator_GetJWT(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	futureExpiry := fixedTime.Add(1 * time.Hour)

	tests := []struct {
		name           string
		cachedToken    *CachedToken
		httpResponse   *http.Response
		httpErr        error
		decoderExpiry  time.Time
		decoderErr     error
		wantToken      string
		wantErr        bool
		errContains    string
		expectHTTPCall bool
	}{
		{
			name: "cache hit - valid token",
			cachedToken: &CachedToken{
				AccessToken: "cached-jwt-token",
				ExpiresAt:   fixedTime.Add(30 * time.Minute), // Expires in 30 mins
			},
			wantToken:      "cached-jwt-token",
			wantErr:        false,
			expectHTTPCall: false,
		},
		{
			name: "cache miss - fetch new token",
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":"new-jwt-token"}`))),
			},
			decoderExpiry:  futureExpiry,
			wantToken:      "new-jwt-token",
			wantErr:        false,
			expectHTTPCall: true,
		},
		{
			name: "cache expired - fetch new token",
			cachedToken: &CachedToken{
				AccessToken: "expired-token",
				ExpiresAt:   fixedTime.Add(-10 * time.Minute), // Expired 10 mins ago
			},
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":"fresh-jwt-token"}`))),
			},
			decoderExpiry:  futureExpiry,
			wantToken:      "fresh-jwt-token",
			wantErr:        false,
			expectHTTPCall: true,
		},
		{
			name: "cache expiring soon - fetch new token",
			cachedToken: &CachedToken{
				AccessToken: "almost-expired-token",
				ExpiresAt:   fixedTime.Add(3 * time.Minute), // Expires in 3 mins (within 5 min buffer)
			},
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":"refreshed-jwt-token"}`))),
			},
			decoderExpiry:  futureExpiry,
			wantToken:      "refreshed-jwt-token",
			wantErr:        false,
			expectHTTPCall: true,
		},
		{
			name:           "http error",
			httpErr:        &mockError{msg: "connection refused"},
			wantErr:        true,
			errContains:    "auth request failed",
			expectHTTPCall: true,
		},
		{
			name: "auth error response",
			httpResponse: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":"invalid api key"}`))),
			},
			wantErr:        true,
			errContains:    "authentication failed",
			expectHTTPCall: true,
		},
		{
			name: "empty access token",
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":""}`))),
			},
			wantErr:        true,
			errContains:    "empty access token",
			expectHTTPCall: true,
		},
		{
			name: "invalid json response",
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`not json`))),
			},
			wantErr:        true,
			errContains:    "parse auth response",
			expectHTTPCall: true,
		},
		{
			name: "decoder error - fallback to default expiry",
			httpResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":"token-with-bad-exp"}`))),
			},
			decoderErr:     &mockError{msg: "invalid JWT"},
			wantToken:      "token-with-bad-exp",
			wantErr:        false,
			expectHTTPCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockHTTPClient{
				Response: tt.httpResponse,
				Err:      tt.httpErr,
			}

			cache := &TokenCache{Tokens: make(map[string]CachedToken)}
			if tt.cachedToken != nil {
				cache.Tokens["dev"] = *tt.cachedToken
			}

			mockStore := &MockTokenStore{Cache: cache}
			mockDecoder := &MockJWTDecoder{
				ExpiresAt: tt.decoderExpiry,
				Err:       tt.decoderErr,
			}

			auth := &Authenticator{
				Client:  mockClient,
				Store:   mockStore,
				Decoder: mockDecoder,
				TimeNow: func() time.Time { return fixedTime },
			}

			token, err := auth.GetJWT("devops.acme.simple.dev", "test-api-key", "dev")

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetJWT() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("GetJWT() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("GetJWT() unexpected error = %v", err)
				return
			}

			if token != tt.wantToken {
				t.Errorf("GetJWT() = %q, want %q", token, tt.wantToken)
			}

			// Verify HTTP call was made or not
			if tt.expectHTTPCall && len(mockClient.Requests) == 0 {
				t.Error("GetJWT() expected HTTP call, but none made")
			}
			if !tt.expectHTTPCall && len(mockClient.Requests) > 0 {
				t.Error("GetJWT() unexpected HTTP call made")
			}

			// Verify headers on HTTP request
			if tt.expectHTTPCall && len(mockClient.Requests) > 0 {
				req := mockClient.Requests[0]
				if req.Header.Get("x-api-key") != "test-api-key" {
					t.Errorf("HTTP request missing x-api-key header")
				}
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("HTTP request missing Content-Type header")
				}
			}
		})
	}
}

func TestDefaultJWTDecoder_DecodeExpiry(t *testing.T) {
	decoder := &DefaultJWTDecoder{}

	tests := []struct {
		name        string
		token       string
		wantExp     int64
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid JWT with exp",
			token:   createTestJWT(1704110400), // 2024-01-01 12:00:00 UTC
			wantExp: 1704110400,
			wantErr: false,
		},
		{
			name:        "invalid format - not enough parts",
			token:       "only.two",
			wantErr:     true,
			errContains: "invalid JWT format",
		},
		{
			name:        "invalid base64",
			token:       "header.!!!invalid!!!.signature",
			wantErr:     true,
			errContains: "decode JWT payload",
		},
		{
			name: "missing exp claim",
			token: func() string {
				header := base64.URLEncoding.EncodeToString([]byte(`{"alg":"EdDSA"}`))
				payload := base64.URLEncoding.EncodeToString([]byte(`{"sub":"test"}`))
				sig := base64.URLEncoding.EncodeToString([]byte("sig"))
				return header + "." + payload + "." + sig
			}(),
			wantErr:     true,
			errContains: "exp claim not found",
		},
		{
			name: "invalid JSON in payload",
			token: func() string {
				header := base64.URLEncoding.EncodeToString([]byte(`{"alg":"EdDSA"}`))
				payload := base64.URLEncoding.EncodeToString([]byte(`not json`))
				sig := base64.URLEncoding.EncodeToString([]byte("sig"))
				return header + "." + payload + "." + sig
			}(),
			wantErr:     true,
			errContains: "parse JWT claims",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expiry, err := decoder.DecodeExpiry(tt.token)

			if tt.wantErr {
				if err == nil {
					t.Errorf("DecodeExpiry() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("DecodeExpiry() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("DecodeExpiry() unexpected error = %v", err)
				return
			}

			if expiry.Unix() != tt.wantExp {
				t.Errorf("DecodeExpiry() = %d, want %d", expiry.Unix(), tt.wantExp)
			}
		})
	}
}

func TestBase64URLDecode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"no padding needed", "aGVsbG8", "hello", false},
		{"needs one pad", "aGVsbG9v", "helloo", false},
		{"needs two pads", "aGVsbA", "hell", false},
		{"with url safe chars", "YWJjLV8", "abc-_", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := base64URLDecode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("base64URLDecode() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("base64URLDecode() unexpected error = %v", err)
				return
			}
			if string(result) != tt.want {
				t.Errorf("base64URLDecode() = %q, want %q", string(result), tt.want)
			}
		})
	}
}

func TestAuthenticator_ClearCache(t *testing.T) {
	mockStore := &MockTokenStore{
		Cache: &TokenCache{
			Tokens: map[string]CachedToken{
				"dev":     {AccessToken: "dev-token"},
				"staging": {AccessToken: "staging-token"},
			},
		},
	}

	auth := &Authenticator{
		Store: mockStore,
		cache: mockStore.Cache,
	}

	err := auth.ClearCache("dev")
	if err != nil {
		t.Errorf("ClearCache() unexpected error = %v", err)
	}

	if _, ok := mockStore.Cache.Tokens["dev"]; ok {
		t.Error("ClearCache() did not remove dev token")
	}

	if _, ok := mockStore.Cache.Tokens["staging"]; !ok {
		t.Error("ClearCache() incorrectly removed staging token")
	}

	if mockStore.SaveCalls != 1 {
		t.Errorf("ClearCache() save calls = %d, want 1", mockStore.SaveCalls)
	}
}

func TestAuthenticator_ClearCache_NilCache(t *testing.T) {
	mockStore := &MockTokenStore{}
	auth := &Authenticator{
		Store: mockStore,
		cache: nil,
	}

	err := auth.ClearCache("dev")
	if err != nil {
		t.Errorf("ClearCache() unexpected error = %v", err)
	}
}

func TestFileTokenStore_LoadSave(t *testing.T) {
	dir := t.TempDir()
	store := &FileTokenStore{ConfigDir: dir}

	// Initially empty
	cache, err := store.Load()
	if err != nil {
		t.Fatalf("Load() unexpected error = %v", err)
	}
	if cache.Tokens == nil {
		t.Error("Load() returned nil Tokens map")
	}

	// Save some tokens
	cache.Tokens["dev"] = CachedToken{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	err = store.Save(cache)
	if err != nil {
		t.Fatalf("Save() unexpected error = %v", err)
	}

	// Reload and verify
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after save unexpected error = %v", err)
	}

	if loaded.Tokens["dev"].AccessToken != "test-token" {
		t.Errorf("Loaded token = %q, want %q", loaded.Tokens["dev"].AccessToken, "test-token")
	}

	// Verify file permissions
	path := filepath.Join(dir, "tokens.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	// On Unix, check permissions are 0600
	if info.Mode().Perm() != 0600 {
		t.Errorf("File permissions = %o, want %o", info.Mode().Perm(), 0600)
	}
}

func TestFileTokenStore_Load_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	store := &FileTokenStore{ConfigDir: dir}

	// Write corrupted JSON
	path := filepath.Join(dir, "tokens.json")
	_ = os.WriteFile(path, []byte("not valid json"), 0600)

	// Should return empty cache, not error
	cache, err := store.Load()
	if err != nil {
		t.Fatalf("Load() unexpected error for corrupted file = %v", err)
	}
	if len(cache.Tokens) != 0 {
		t.Errorf("Load() should return empty cache for corrupted file")
	}
}

func TestFileTokenStore_Load_NullTokens(t *testing.T) {
	dir := t.TempDir()
	store := &FileTokenStore{ConfigDir: dir}

	// Write valid JSON with null tokens
	path := filepath.Join(dir, "tokens.json")
	_ = os.WriteFile(path, []byte(`{"tokens":null}`), 0600)

	cache, err := store.Load()
	if err != nil {
		t.Fatalf("Load() unexpected error = %v", err)
	}
	if cache.Tokens == nil {
		t.Error("Load() should initialize nil Tokens to empty map")
	}
}

func TestNewAuthenticator(t *testing.T) {
	auth := NewAuthenticator()
	if auth == nil {
		t.Fatal("NewAuthenticator() returned nil")
	}
	if auth.Client == nil {
		t.Error("NewAuthenticator() Client is nil")
	}
	if auth.Store == nil {
		t.Error("NewAuthenticator() Store is nil")
	}
	if auth.Decoder == nil {
		t.Error("NewAuthenticator() Decoder is nil")
	}
	if auth.TimeNow == nil {
		t.Error("NewAuthenticator() TimeNow is nil")
	}
}

func TestAuthenticator_GetJWT_StoreLoadError(t *testing.T) {
	mockClient := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":"new-token"}`))),
		},
	}

	mockStore := &MockTokenStore{
		LoadErr: &mockError{msg: "load error"},
	}

	auth := &Authenticator{
		Client:  mockClient,
		Store:   mockStore,
		Decoder: &MockJWTDecoder{ExpiresAt: time.Now().Add(1 * time.Hour)},
		TimeNow: time.Now,
	}

	// Should still work, just starts fresh
	token, err := auth.GetJWT("devops.acme.simple.dev", "test-api-key", "dev")
	if err != nil {
		t.Errorf("GetJWT() unexpected error = %v", err)
	}
	if token != "new-token" {
		t.Errorf("GetJWT() = %q, want %q", token, "new-token")
	}
}

func TestFileTokenStore_tokenCachePath(t *testing.T) {
	// Test with custom ConfigDir
	store := &FileTokenStore{ConfigDir: "/custom/path"}
	path := store.tokenCachePath()
	if path != "/custom/path/tokens.json" {
		t.Errorf("tokenCachePath() = %q, want %q", path, "/custom/path/tokens.json")
	}

	// Test with default (home directory)
	store = &FileTokenStore{}
	path = store.tokenCachePath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".simple", "tokens.json")
	if path != expected {
		t.Errorf("tokenCachePath() = %q, want %q", path, expected)
	}
}

func TestExchangeAPIKeyForJWT_RequestFormat(t *testing.T) {
	mockClient := &MockHTTPClient{
		Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"access_token":"jwt"}`))),
		},
	}

	auth := &Authenticator{
		Client:  mockClient,
		Store:   &MockTokenStore{},
		Decoder: &MockJWTDecoder{ExpiresAt: time.Now().Add(1 * time.Hour)},
		TimeNow: time.Now,
	}

	_, _ = auth.GetJWT("devops.tenant.simple.dev", "my-api-key", "prod")

	if len(mockClient.Requests) != 1 {
		t.Fatalf("Expected 1 request, got %d", len(mockClient.Requests))
	}

	req := mockClient.Requests[0]

	// Check URL
	if req.URL.String() != "https://devops.tenant.simple.dev/api/auth/token" {
		t.Errorf("Request URL = %q, want %q", req.URL.String(), "https://devops.tenant.simple.dev/api/auth/token")
	}

	// Check method
	if req.Method != "POST" {
		t.Errorf("Request Method = %q, want %q", req.Method, "POST")
	}

	// Check headers
	if req.Header.Get("x-api-key") != "my-api-key" {
		t.Errorf("x-api-key header = %q, want %q", req.Header.Get("x-api-key"), "my-api-key")
	}
}
