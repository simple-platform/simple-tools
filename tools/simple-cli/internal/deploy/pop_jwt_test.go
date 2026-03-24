package deploy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSignPopJWT(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	token, err := SignPopJWT(priv, "000001f097af4c")
	if err != nil {
		t.Fatalf("SignPopJWT() error = %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}

	// Verify header
	headerBytes, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var header map[string]string
	_ = json.Unmarshal(headerBytes, &header)
	if header["alg"] != "EdDSA" || header["typ"] != "JWT" || header["kid"] != "KEY000001f097af4c" {
		t.Errorf("unexpected header: %v", header)
	}

	// Verify claims
	payloadBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]interface{}
	_ = json.Unmarshal(payloadBytes, &claims)

	if claims["sub"] != "000001f097af4c" {
		t.Errorf("sub = %v, want 000001f097af4c", claims["sub"])
	}
	if claims["jti"] == "" {
		t.Error("jti is empty")
	}

	exp := int64(claims["exp"].(float64))
	iat := int64(claims["iat"].(float64))
	if exp-iat != 60 {
		t.Errorf("exp-iat = %d, want 60", exp-iat)
	}
	if exp <= time.Now().Unix() {
		t.Error("exp is not in the future")
	}

	// Verify EdDSA signature
	signingInput := parts[0] + "." + parts[1]
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if !ed25519.Verify(pub, []byte(signingInput), sig) {
		t.Error("EdDSA signature verification failed")
	}
}

func TestSignPopJWT_UniqueJTI(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	t1, _ := SignPopJWT(priv, "abc")
	t2, _ := SignPopJWT(priv, "abc")

	jti := func(token string) string {
		parts := strings.Split(token, ".")
		b, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var claims map[string]interface{}
		_ = json.Unmarshal(b, &claims)
		return claims["jti"].(string)
	}

	if jti(t1) == jti(t2) {
		t.Error("two SignPopJWT calls produced the same jti — jti must be unique per call")
	}
}

func TestParseIDSuffix(t *testing.T) {
	tests := []struct {
		name       string
		rawKey     string
		wantSuffix string
		wantErr    bool
	}{
		{
			name:       "valid key",
			rawKey:     "si_000001f097af4c" + strings.Repeat("a", 64),
			wantSuffix: "000001f097af4c",
		},
		{
			name:    "too short",
			rawKey:  "si_abc",
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			rawKey:  "sk_" + strings.Repeat("a", 80),
			wantErr: true,
		},
		{
			name:    "exactly prefix + 64 random (no id_suffix)",
			rawKey:  "si_" + strings.Repeat("a", 64),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix, err := ParseIDSuffix(tt.rawKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIDSuffix() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && suffix != tt.wantSuffix {
				t.Errorf("suffix = %q, want %q", suffix, tt.wantSuffix)
			}
		})
	}
}
