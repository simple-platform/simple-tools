package deploy

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lithammer/shortuuid/v4"
)

// SignPopJWT creates a compact EdDSA-signed JWT for Proof-of-Possession authentication.
//
// The resulting token is intentionally short-lived (60 seconds) and must NEVER be
// cached. A fresh token is signed on every GetJWT call.
//
// Header: {"alg":"EdDSA","typ":"JWT"}
// Claims: {"sub":"KEY"+idSuffix, "jti":<shortuuid>, "iat":<now>, "exp":<now+60>}
//
// The idSuffix is the bare API key identifier - the "KEY" DB prefix IS re-attached
// for the "sub" claim to provide full identity alignment.
func SignPopJWT(privateKey ed25519.PrivateKey, idSuffix string) (string, error) {
	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "EdDSA",
		"typ": "JWT",
		"kid": "KEY" + idSuffix,
	})
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	now := time.Now().Unix()
	claimsJSON, _ := json.Marshal(map[string]interface{}{
		"sub": "KEY" + idSuffix, // Prefixed, e.g. "KEY000001f097af4c"
		"jti": shortuuid.New(),  // Base57 short UUID
		"iat": now,
		"exp": now + 60,
	})
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64
	sig := ed25519.Sign(privateKey, []byte(signingInput))

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ParseIDSuffix extracts the id_suffix from a raw API key.
//
// Raw key format: "si_<id_suffix><64_char_random>"
// Example:        "si_000001f097af4c" + 64 random chars
//
// The id_suffix is used as:
//   - The keypair directory name (~/.simple/keys/<id_suffix>/)
//   - The "sub" claim in the PoP JWT
//   - The prefix in the PoP auth string ("si_<id_suffix>.<pop_jwt>")
//
// The "KEY" DB prefix is deliberately NOT re-attached — it has no
// meaning outside the database layer.
func ParseIDSuffix(rawKey string) (string, error) {
	const prefix = "si_"
	const randomLen = 64

	if len(rawKey) <= len(prefix)+randomLen {
		return "", fmt.Errorf("api key too short to be valid")
	}
	if rawKey[:3] != prefix {
		return "", fmt.Errorf("api key must start with 'si_'")
	}

	rest := rawKey[len(prefix):]
	suffixLen := len(rest) - randomLen
	if suffixLen <= 0 {
		return "", fmt.Errorf("api key id_suffix is empty")
	}

	return rest[:suffixLen], nil
}
