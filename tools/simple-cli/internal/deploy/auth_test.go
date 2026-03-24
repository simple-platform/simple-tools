package deploy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// MockHTTPClient implements HTTPClient for testing.
type MockHTTPClient struct {
	Response  *http.Response
	Responses []*http.Response // For sequential responses
	Errs      []error          // For sequential errors
	Requests  []*http.Request
	OnDo      func(req *http.Request)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.Requests = append(m.Requests, req)
	if m.OnDo != nil {
		m.OnDo(req)
	}

	if len(m.Errs) > 0 {
		err := m.Errs[0]
		if len(m.Errs) > 1 {
			m.Errs = m.Errs[1:]
		} else {
			m.Errs = nil
		}
		if err != nil {
			return nil, err
		}
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
	header := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"%s","typ":"at+jwt","kid":"KEYtestid"}`, alg)))
	payload := base64.URLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `,"sub":"test"}`))
	signature := base64.URLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + signature
}

func TestAuthenticator_GetJWT(t *testing.T) {
	tmpConfig := t.TempDir()
	t.Setenv("HOME", tmpConfig)

	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	futureExpiry := fixedTime.Add(1 * time.Hour)
	// 32-byte key base64url encoded
	validJWKS := `{"keys":[{"kty":"OKP","kid":"KEYtestid","crv":"Ed25519","x":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"}]}`

	validAPIKey := "si_testid" + strings.Repeat("a", 64)

	tests := []struct {
		name            string
		cachedToken     *CachedToken
		httpResponses   []*http.Response
		httpErrs        []error
		decoderExpiry   time.Time
		decoderErr      error
		verifyErr       bool
		wantToken       string
		wantErr         bool
		errContains     string
		expectHTTPCalls int
		setupClient     func(m *MockHTTPClient, cancel context.CancelFunc)
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
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))},                                                                         // Enroll
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWT(futureExpiry.Unix())))))}, // Login
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(validJWKS)))},                                                                // JWKS
			},
			decoderExpiry:   futureExpiry,
			wantToken:       createTestJWT(futureExpiry.Unix()),
			wantErr:         false,
			expectHTTPCalls: 3,
		},
		{
			name: "verification failed",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))},                                                                         // Enroll
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWT(futureExpiry.Unix())))))}, // Login
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(validJWKS)))},                                                                // JWKS
			},
			verifyErr:       true,
			wantErr:         true,
			errContains:     "invalid JWT signature",
			expectHTTPCalls: 3,
		},
		{
			name:            "http error on enroll network",
			httpErrs:        []error{&mockError{msg: "connection refused"}, &mockError{msg: "connection refused"}},
			wantErr:         true,
			errContains:     "enrollment failed after retries",
			expectHTTPCalls: 2,
		},
		{
			name: "http error on enroll 500",
			httpResponses: []*http.Response{
				{StatusCode: 500, Body: io.NopCloser(bytes.NewReader([]byte("server error")))},
				{StatusCode: 500, Body: io.NopCloser(bytes.NewReader([]byte("server error")))},
			},
			wantErr:         true,
			errContains:     "enrollment failed after retries",
			expectHTTPCalls: 2,
		},
		{
			name:            "context canceled between retries",
			httpErrs:        []error{&mockError{msg: "connection refused"}, &mockError{msg: "connection refused"}},
			wantErr:         true,
			errContains:     "enrollment canceled",
			expectHTTPCalls: 1, // Only 1 call because context cancels before retry
			setupClient: func(m *MockHTTPClient, cancel context.CancelFunc) {
				m.OnDo = func(req *http.Request) {
					cancel()
				}
			},
		},
		{
			name: "http error on jwks",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))},                           // Enroll
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"access_token":"token"}`)))}, // Login
			},
			wantErr:         true,
			expectHTTPCalls: 3,
		},
		{
			name: "unsupported algorithm",
			httpResponses: []*http.Response{
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))},                                                                                         // Enroll
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWTWithAlg(futureExpiry.Unix(), "HS256")))))}, // Login
				{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(validJWKS)))},                                                                                // JWKS
			},
			wantErr:         true,
			errContains:     "unsupported algorithm: HS256",
			expectHTTPCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear keystore dir for each test so enrollment triggers
			_ = os.RemoveAll(filepath.Join(tmpConfig, ".simple", "keys"))

			// Special handling for the "http error on jwks" case where we want the last call to return 404
			if tt.name == "http error on jwks" {
				tt.httpResponses = []*http.Response{
					{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte{}))},                           // Enroll
					{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte(`{"access_token":"token"}`)))}, // Login
					{StatusCode: 404, Body: io.NopCloser(bytes.NewReader([]byte{}))},                           // JWKS
				}
				tt.errContains = "failed to fetch JWKS"
			}

			mockClient := &MockHTTPClient{
				Responses: tt.httpResponses,
				Errs:      tt.httpErrs,
			}

			cache := &TokenCache{Tokens: make(map[string]CachedToken)}
			if tt.cachedToken != nil {
				cache.Tokens["acme::dev"] = *tt.cachedToken
			}

			mockStore := &MockTokenStore{Cache: cache}
			mockDecoder := &MockJWTDecoder{
				ExpiresAt: tt.decoderExpiry,
				Err:       tt.decoderErr,
			}

			auth := &Authenticator{
				Client:          mockClient,
				Store:           mockStore,
				Decoder:         mockDecoder,
				TimeNow:         func() time.Time { return fixedTime },
				VerifySignature: func(_, _, _ []byte) bool { return !tt.verifyErr },
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.setupClient != nil {
				tt.setupClient(mockClient, cancel)
			}

			token, err := auth.GetJWT(ctx, "devops.acme.simple.dev", validAPIKey, "acme::dev")

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetJWT() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
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
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
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
				"acme::dev":     {AccessToken: "dev-token"},
				"acme::staging": {AccessToken: "staging-token"},
			},
		},
	}

	auth := &Authenticator{
		Store: mockStore,
		cache: mockStore.Cache,
	}

	err := auth.ClearCache("acme::dev")
	if err != nil {
		t.Errorf("ClearCache() unexpected error = %v", err)
	}

	if _, ok := mockStore.Cache.Tokens["acme::dev"]; ok {
		t.Error("ClearCache() did not remove dev token")
	}

	if _, ok := mockStore.Cache.Tokens["acme::staging"]; !ok {
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

	err := auth.ClearCache("acme::dev")
	if err != nil {
		t.Errorf("ClearCache() unexpected error = %v", err)
	}

	if mockStore.SaveCalls != 1 {
		t.Errorf("ClearCache() save calls = %d, want 1", mockStore.SaveCalls)
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
	cache.Tokens["acme::dev"] = CachedToken{
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

	if loaded.Tokens["acme::dev"].AccessToken != "test-token" {
		t.Errorf("Loaded token = %q, want %q", loaded.Tokens["acme::dev"].AccessToken, "test-token")
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
	tmpConfig := t.TempDir()
	t.Setenv("HOME", tmpConfig)
	validAPIKey := "si_storeerr" + strings.Repeat("a", 64)

	mockClient := &MockHTTPClient{
		Responses: []*http.Response{
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte{}))}, // Enroll
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, createTestJWT(time.Now().Add(1*time.Hour).Unix())))))},
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"keys":[{"kty":"OKP","kid":"KEYtestid","crv":"Ed25519","x":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"}]}`)))},
		},
	}

	mockStore := &MockTokenStore{
		LoadErr: &mockError{msg: "load error"},
	}

	auth := &Authenticator{
		Client:          mockClient,
		Store:           mockStore,
		Decoder:         &MockJWTDecoder{ExpiresAt: time.Now().Add(1 * time.Hour)},
		TimeNow:         time.Now,
		VerifySignature: func(_, _, _ []byte) bool { return true },
	}

	// Should still work, just starts fresh
	token, err := auth.GetJWT(context.Background(), "devops.acme.simple.dev", validAPIKey, "acme::dev")
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

func TestEnrollAndAuthenticate_RequestFormat(t *testing.T) {
	tmpConfig := t.TempDir()
	t.Setenv("HOME", tmpConfig)
	validAPIKey := "si_mytestid" + strings.Repeat("a", 64)

	futureToken := createTestJWT(time.Now().Add(1 * time.Hour).Unix())
	mockClient := &MockHTTPClient{
		Responses: []*http.Response{
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte{}))},                                                                                                              // Enroll
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"access_token":"%s"}`, futureToken))))},                                                             // Login
			{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{"keys":[{"kty":"OKP","kid":"KEYmytestid","crv":"Ed25519","x":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI"}]}`)))}, // JWKS
		},
	}

	auth := &Authenticator{
		Client:          mockClient,
		Store:           &MockTokenStore{},
		Decoder:         &MockJWTDecoder{ExpiresAt: time.Now().Add(1 * time.Hour)},
		TimeNow:         time.Now,
		VerifySignature: func(_, _, _ []byte) bool { return true },
	}

	_, _ = auth.GetJWT(context.Background(), "devops.tenant.simple.dev", validAPIKey, "acme::prod")

	if len(mockClient.Requests) < 2 {
		t.Fatalf("Expected at least 2 requests, got %d", len(mockClient.Requests))
	}

	// Check Enroll request
	enrollReq := mockClient.Requests[0]
	if enrollReq.URL.String() != "https://devops.tenant.simple.dev/auth/api-key/enroll" {
		t.Errorf("Request URL = %q, want %q", enrollReq.URL.String(), "https://devops.tenant.simple.dev/auth/api-key/enroll")
	}
	if enrollReq.Method != "POST" {
		t.Errorf("Request Method = %q, want %q", enrollReq.Method, "POST")
	}
	enrollBody, _ := io.ReadAll(enrollReq.Body)
	if !strings.Contains(string(enrollBody), `"api_key":"`+validAPIKey+`"`) {
		t.Errorf("Enroll body missing api_key, got: %s", string(enrollBody))
	}
	if !strings.Contains(string(enrollBody), `"public_key"`) || !strings.Contains(string(enrollBody), `"kid":"KEYmytestid"`) {
		t.Errorf("Enroll body missing public_key or correct kid, got: %s", string(enrollBody))
	}

	// Check Login request
	loginReq := mockClient.Requests[1]
	if loginReq.URL.String() != "https://devops.tenant.simple.dev/auth/login" {
		t.Errorf("Request URL = %q, want %q", loginReq.URL.String(), "https://devops.tenant.simple.dev/auth/login")
	}
	if loginReq.Method != "POST" {
		t.Errorf("Request Method = %q, want %q", loginReq.Method, "POST")
	}
	loginBody, _ := io.ReadAll(loginReq.Body)
	if !strings.Contains(string(loginBody), `"api_key":"si_mytestid.`) {
		t.Errorf("Login body missing PoP JWT format, got: %s", string(loginBody))
	}

	var loginMap map[string]string
	if err := json.Unmarshal(loginBody, &loginMap); err == nil {
		parts := strings.Split(loginMap["api_key"], ".")
		if len(parts) == 4 { // si_prefix.header.payload.sig
			// Verify header kid in PoP JWT
			headerBytes, err := base64URLDecode(parts[1])
			if err == nil {
				var header map[string]string
				_ = json.Unmarshal(headerBytes, &header)
				if header["kid"] != "KEYmytestid" {
					t.Errorf("PoP JWT header kid = %q, want KEYmytestid", header["kid"])
				}
			}

			payloadBytes, err := base64URLDecode(parts[2])
			if err == nil {
				payload := string(payloadBytes)
				if !strings.Contains(payload, `"jti":"`) {
					t.Errorf("PoP JWT payload missing 'jti' claim: %s", payload)
				}
				if !strings.Contains(payload, `"sub":"KEYmytestid"`) {
					t.Errorf("PoP JWT payload missing correct sub, got: %s", payload)
				}
			}
		} else {
			t.Errorf("Invalid PoP JWT format parts count: %d", len(parts))
		}
	} else {
		t.Errorf("Failed to unmarshal login body: %v", err)
	}
}
