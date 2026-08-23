// Package auth verifies Supabase-issued JWTs on incoming requests.
// Supabase projects using the newer "JWT signing keys" feature sign
// access tokens with ES256 (asymmetric) rather than the legacy HS256
// shared secret — this middleware verifies against Supabase's public
// JWKS endpoint, which is what's needed for ES256-signed tokens.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const UserIDKey contextKey = "user_id"

type jwksKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

// keyCache fetches and caches Supabase's public signing keys (JWKS).
// Keys are refreshed at most every 10 minutes, or immediately if a
// token references a "kid" we haven't seen yet (e.g. after Supabase
// rotates keys).
type keyCache struct {
	mu        sync.RWMutex
	jwksURL   string
	keys      map[string]*ecdsa.PublicKey
	fetchedAt time.Time
}

func newKeyCache(supabaseURL string) *keyCache {
	return &keyCache{
		jwksURL: strings.TrimRight(supabaseURL, "/") + "/auth/v1/.well-known/jwks.json",
		keys:    make(map[string]*ecdsa.PublicKey),
	}
}

func (c *keyCache) get(kid string) (*ecdsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	stale := time.Since(c.fetchedAt) > 10*time.Minute
	c.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	if err := c.refresh(); err != nil {
		if ok {
			// Refresh failed but we still have a previously-seen key
			// for this kid — better to accept it than hard-fail every
			// request just because a JWKS fetch timed out.
			return key, nil
		}
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok = c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("no matching JWKS key for kid %q", kid)
	}
	return key, nil
}

func (c *keyCache) refresh() error {
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(c.jwksURL)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	var parsed jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*ecdsa.PublicKey, len(parsed.Keys))
	for _, k := range parsed.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" {
			continue // only ES256 (P-256) keys are supported
		}
		pub, err := ecPublicKeyFromCoords(k.X, k.Y)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}

	c.mu.Lock()
	c.keys = keys
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	return nil
}

func ecPublicKeyFromCoords(xB64, yB64 string) (*ecdsa.PublicKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(xB64)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yB64)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// Middleware validates the Authorization: Bearer <jwt> header issued
// by Supabase Auth, verifying the ES256 signature against Supabase's
// public JWKS, and injects the authenticated user's id (the "sub"
// claim) into the request context.
func Middleware(supabaseURL string) func(http.Handler) http.Handler {
	cache := newKeyCache(supabaseURL)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				kid, _ := t.Header["kid"].(string)
				if kid == "" {
					return nil, fmt.Errorf("token missing kid header")
				}
				return cache.get(kid)
			}, jwt.WithValidMethods([]string{"ES256"}))

			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid claims")
				return
			}
			sub, ok := claims["sub"].(string)
			if !ok || sub == "" {
				writeError(w, http.StatusUnauthorized, "invalid subject claim")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, sub)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the authenticated user's id set by Middleware.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(UserIDKey).(string)
	return v, ok
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}