package deploy

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// MockHTTPClient implements HTTPClient for testing.
type MockHTTPClient struct {
	Response  *http.Response
	Responses []*http.Response // For sequential responses
	Err       error
	Requests  []*http.Request
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.Requests = append(m.Requests, req)
	if m.Err != nil {
		return nil, m.Err
	}
	// Use sequential responses if available
	if len(m.Responses) > 0 {
		resp := m.Responses[0]
		m.Responses = m.Responses[1:]
		return resp, nil
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
	return createTestJWTWithAlg(exp, "EdDSA")
}

func createTestJWTWithAlg(exp int64, alg string) string {
	header := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"%s","typ":"at+jwt","kid":"test-key-id"}`, alg)))
	payload := base64.URLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `,"sub":"test"}`))
	signature := base64.URLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + signature
}

func TestAuthenticator_GetJWT(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	futureExpiry := fixedTime.Add(1 * time.Hour)
	// 32-byte key base64url encoded
	validJWKS := `{"keys":[{"kty":"OKP","kid":"test-key-id","crv":"Ed25519","x":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"}]}`

	// Mock verification to always pass by default
	originalVerify := VerifyEd25519
	VerifyEd25519 = func(pub, msg, sig []byte) bool { return true }
	defer func() { VerifyEd25519 = originalVerify }()

	tests := []struct {
		name            string
		cachedToken     *CachedToken
		httpResponses   []*http.Response
		httpErr         error
		decoderExpiry   time.Time
		decoderErr      error
		verifyErr       bool
		wantToken       string
		wantErr         bool
		errContains     string
		expectHTTPCalls int
	}{
		{
			name: "cache hit - valid token",
			cachedToken: &CachedToken{
				AccessToken: "cached-jwt-token",
				ExpiresAt:   fixedTime.Add(30 * time.Minute),
			},
			wantToken:       "cached-jwt-token",
			wantErr:         false,
			expectHTTPCalls: 0,
		},
		{
			name: "cache miss - fetch new token and verify",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWT(futureExpiry.Unix())))))},
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(validJWKS)))},
			},
			decoderExpiry:   futureExpiry,
			wantToken:       createTestJWT(futureExpiry.Unix()),
			wantErr:         false,
			expectHTTPCalls: 2,
		},
		{
			name: "verification failed",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWT(futureExpiry.Unix())))))},
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(validJWKS)))},
			},
			verifyErr:       true,
			wantErr:         true,
			errContains:     "invalid JWT signature",
			expectHTTPCalls: 2,
		},
		{
			name:            "http error on token",
			httpErr:         &mockError{msg: "connection refused"},
			wantErr:         true,
			errContains:     "auth request failed",
			expectHTTPCalls: 1,
		},
		{
			name: "http error on jwks",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"access_token":"token"}`)))},
			},
			wantErr:         true,
			expectHTTPCalls: 2,
		},
		{
			name: "unsupported algorithm",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWTWithAlg(futureExpiry.Unix(), "HS256")))))},
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(validJWKS)))},
			},
			wantErr:         true,
			errContains:     "unsupported algorithm: HS256",
			expectHTTPCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Special handling for the "http error on jwks" case where we want the second call to return 404
			if tt.name == "http error on jwks" {
				tt.httpResponses = []*http.Response{
					{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"access_token":"token"}`)))},
					{StatusCode: 404, Body: io.NopCloser(bytes.NewReader([]byte{}))},
				}
				tt.errContains = "failed to fetch JWKS"
			}

			mockClient := &MockHTTPClient{
				Responses: tt.httpResponses,
				Err:       tt.httpErr,
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

			// Mock verify logic for this run
			if tt.verifyErr {
				VerifyEd25519 = func(_, _, _ []byte) bool { return false }
			} else {
				VerifyEd25519 = func(_, _, _ []byte) bool { return true }
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

			if len(mockClient.Requests) != tt.expectHTTPCalls {
				t.Errorf("GetJWT() HTTP calls = %d, want %d", len(mockClient.Requests), tt.expectHTTPCalls)
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
		Responses: []*http.Response{
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWT(time.Now().Add(1*time.Hour).Unix())))))},
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"keys":[{"kty":"OKP","kid":"test-key-id","crv":"Ed25519","x":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"}]}`)))},
		},
	}

	// Mock verification to pass
	originalVerify := VerifyEd25519
	VerifyEd25519 = func(pub, msg, sig []byte) bool { return true }
	defer func() { VerifyEd25519 = originalVerify }()

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
	if token != createTestJWT(time.Now().Add(1*time.Hour).Unix()) {
		t.Errorf("GetJWT() token mismatch")
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
	futureToken := createTestJWT(time.Now().Add(1 * time.Hour).Unix())
	mockClient := &MockHTTPClient{
		Responses: []*http.Response{
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, futureToken))))},
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"keys":[{"kty":"OKP","kid":"test-key-id","crv":"Ed25519","x":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"}]}`)))},
		},
	}

	// Mock verification
	originalVerify := VerifyEd25519
	VerifyEd25519 = func(pub, msg, sig []byte) bool { return true }
	defer func() { VerifyEd25519 = originalVerify }()

	auth := &Authenticator{
		Client:  mockClient,
		Store:   &MockTokenStore{},
		Decoder: &MockJWTDecoder{ExpiresAt: time.Now().Add(1 * time.Hour)},
		TimeNow: time.Now,
	}

	_, _ = auth.GetJWT("devops.tenant.simple.dev", "my-api-key", "prod")

	if len(mockClient.Requests) < 1 {
		t.Fatalf("Expected at least 1 request, got %d", len(mockClient.Requests))
	}

	req := mockClient.Requests[0]

	// Check URL
	if req.URL.String() != "https://devops.tenant.simple.dev/auth/login" {
		t.Errorf("Request URL = %q, want %q", req.URL.String(), "https://devops.tenant.simple.dev/auth/login")
	}

	// Check method
	if req.Method != "POST" {
		t.Errorf("Request Method = %q, want %q", req.Method, "POST")
	}

	// Check headers
	if req.Header.Get("x-api-key") != "" {
		t.Errorf("x-api-key header should be empty, got %q", req.Header.Get("x-api-key"))
	}

	// Check body
	body, _ := io.ReadAll(req.Body)
	expectedBody := `{"api_key":"my-api-key"}`
	if string(body) != expectedBody {
		t.Errorf("Request body = %q, want %q", string(body), expectedBody)
	}
}
