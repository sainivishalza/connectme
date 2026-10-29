// Package middleware holds HTTP middleware shared by every feature module.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"connectme/pkg/metrics"

	"github.com/golang-jwt/jwt/v5"
)

const (
	jwtIssuer       = "connectme"
	jwtAudience     = "connectme-api" // access tokens are for this API (and the relay that shares the secret)
	tokenTypeAccess = "access"
)

// sessionCutoff answers, for a user, the moment before which access tokens are
// void (set when their sessions are revoked: sign-out everywhere, password
// change or reset, ban, erasure). It is installed once at startup.
var sessionCutoff atomic.Pointer[func(ctx context.Context, userID string) (time.Time, error)]

// SetSessionCutoff installs the revocation lookup. Without it access tokens are
// purely stateless (valid until they expire).
func SetSessionCutoff(fn func(ctx context.Context, userID string) (time.Time, error)) {
	sessionCutoff.Store(&fn)
}

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxRole
)

// Claims are the access-token claims. Refresh tokens are opaque and live in Redis.
type Claims struct {
	Role string `json:"role"`
	Type string `json:"typ"`
	jwt.RegisteredClaims
}

// SignAccessToken issues a short-lived HS256 access token.
func SignAccessToken(secret []byte, userID, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		Role: role,
		Type: tokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudience},
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

// ParseAccessToken validates signature, algorithm, issuer, expiry and token type.
func ParseAccessToken(secret []byte, raw string) (*Claims, error) {
	var c Claims
	tok, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithAudience(jwtAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	if c.Type != tokenTypeAccess || c.Subject == "" {
		return nil, errors.New("invalid token")
	}
	return &c, nil
}

// Auth rejects requests without a valid bearer access token (401) and stores
// the user id and role in the request context.
func Auth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			raw, ok := strings.CutPrefix(h, "Bearer ")
			if !ok || raw == "" {
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			c, err := ParseAccessToken(secret, raw)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if fn := sessionCutoff.Load(); fn != nil {
				if c.IssuedAt == nil {
					WriteError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				// If the revocation lookup is down we cannot tell whether this
				// token was revoked. Plain member reads keep working (an outage
				// must not sign everyone out and a read only shows what they
				// could already see); anything that changes data, and every
				// staff token, is refused. The outage is counted either way.
				if cut, err := (*fn)(r.Context(), c.Subject); err != nil {
					metrics.Event("session_cutoff_unavailable")
					if c.Role == "moderator" || c.Role == "admin" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
						WriteError(w, http.StatusServiceUnavailable, "service unavailable")
						return
					}
				} else if !cut.IsZero() && c.IssuedAt.Unix() < cut.Unix() {
					WriteError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
			}
			ctx := context.WithValue(r.Context(), ctxUserID, c.Subject)
			ctx = context.WithValue(ctx, ctxRole, c.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID returns the authenticated user id, or "" outside an authenticated request.
func UserID(ctx context.Context) string {
	v, _ := ctx.Value(ctxUserID).(string)
	return v
}

// Role returns the authenticated user's role claim.
func Role(ctx context.Context) string {
	v, _ := ctx.Value(ctxRole).(string)
	return v
}
