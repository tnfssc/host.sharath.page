package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	errTokenMalformed = errors.New("malformed token")
	errTokenSignature = errors.New("invalid token signature")
	errTokenExpired   = errors.New("token expired")
)

type jwtClaims struct {
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Jti string `json:"jti"`
}

var b64 = base64.RawURLEncoding

func signHS256(secret []byte, signingInput string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return b64.EncodeToString(mac.Sum(nil))
}

// mintJWT returns a signed HS256 JWT for sub and its expiry time.
func mintJWT(secret []byte, sub string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	jti := make([]byte, 8)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, fmt.Errorf("rand: %w", err)
	}
	header := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(jwtClaims{Sub: sub, Iat: now.Unix(), Exp: exp.Unix(), Jti: hex.EncodeToString(jti)})
	if err != nil {
		return "", time.Time{}, err
	}
	input := header + "." + b64.EncodeToString(payload)
	return input + "." + signHS256(secret, input), exp, nil
}

// verifyJWT validates the HS256 signature and expiry, returning the subject.
func verifyJWT(secret []byte, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errTokenMalformed
	}
	expected := signHS256(secret, parts[0]+"."+parts[1])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return "", errTokenSignature
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return "", errTokenMalformed
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errTokenMalformed
	}
	if time.Now().Unix() >= claims.Exp {
		return "", errTokenExpired
	}
	return claims.Sub, nil
}

// constantTimeEqual compares two strings without leaking timing.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
