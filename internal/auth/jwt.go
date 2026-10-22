package auth

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type accessClaims struct {
	Role Role `json:"role"`
	jwt.RegisteredClaims
}

// JWTSigner issues and verifies HS256 access tokens.
type JWTSigner struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTSigner(secret string, ttl time.Duration) *JWTSigner {
	return &JWTSigner{secret: []byte(secret), ttl: ttl}
}

func (s *JWTSigner) Issue(p Principal, now time.Time) (string, time.Time, error) {
	exp := now.Add(s.ttl)
	claims := accessClaims{
		Role: p.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(p.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, exp, nil
}

func (s *JWTSigner) Verify(token string, now time.Time) (Principal, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		// Reject non-canonical base64 (altered unused trailing bits), so a
		// token string has exactly one accepted spelling.
		jwt.WithStrictDecoding(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return Principal{}, ErrInvalidAccessToken
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 || !claims.Role.Valid() {
		return Principal{}, ErrInvalidAccessToken
	}
	return Principal{ID: id, Role: claims.Role}, nil
}
