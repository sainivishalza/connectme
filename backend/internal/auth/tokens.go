package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidToken = errors.New("invalid or expired token")
	// ErrTokenReuse means an already-rotated refresh token was presented again:
	// the whole token family is revoked.
	ErrTokenReuse = errors.New("refresh token reuse detected")
)

// rotateScript atomically marks a live token as used. Returns {0} unknown,
// {1, "user|family"} rotated, {2, "user|family"} already used (reuse).
var rotateScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if not v then return {0} end
if string.sub(v, 1, 5) == 'used:' then return {2, string.sub(v, 6)} end
redis.call('SET', KEYS[1], 'used:' .. v, 'KEEPTTL')
return {1, v}
`)

// TokenStore keeps opaque refresh tokens in Redis. Only SHA-256 hashes are
// stored. Tokens belong to a family; rotating issues a new token in the same
// family, and presenting a used token revokes the family.
type TokenStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewTokenStore(rdb *redis.Client, ttl time.Duration) *TokenStore {
	return &TokenStore{rdb: rdb, ttl: ttl}
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (s *TokenStore) tokenKey(token string) string { return "rt:" + hashToken(token) }
func revokedKey(family string) string              { return "rtrevoked:" + family }
func userFamiliesKey(user string) string           { return "ufam:" + user }
func cutoffKey(user string) string                 { return "ucut:" + user }

// cutoffTTL outlives any access token (JWT_ACCESS_EXPIRY is capped at 1 h).
const cutoffTTL = 2 * time.Hour

// SessionCutoff returns the time before which this user's access tokens are
// void, or the zero time when none was set.
func (s *TokenStore) SessionCutoff(ctx context.Context, userID string) (time.Time, error) {
	v, err := s.rdb.Get(ctx, cutoffKey(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(v, 0), nil
}

// Issue creates a refresh token. Pass family="" to start a new family.
func (s *TokenStore) Issue(ctx context.Context, userID, family string) (token, fam string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	if family == "" {
		family = uuid.NewString()
	}
	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, s.tokenKey(token), userID+"|"+family, s.ttl)
	pipe.SAdd(ctx, userFamiliesKey(userID), family)
	pipe.Expire(ctx, userFamiliesKey(userID), s.ttl)
	if _, err = pipe.Exec(ctx); err != nil {
		return "", "", err
	}
	return token, family, nil
}

// Rotate consumes a refresh token and returns its owner and family.
func (s *TokenStore) Rotate(ctx context.Context, token string) (userID, family string, err error) {
	res, err := rotateScript.Run(ctx, s.rdb, []string{s.tokenKey(token)}).Slice()
	if err != nil || len(res) == 0 {
		return "", "", ErrInvalidToken
	}
	code, _ := res[0].(int64)
	if code == 0 || len(res) < 2 {
		return "", "", ErrInvalidToken
	}
	userID, family, ok := strings.Cut(res[1].(string), "|")
	if !ok {
		return "", "", ErrInvalidToken
	}
	if code == 2 {
		_ = s.RevokeFamily(ctx, family)
		return "", "", ErrTokenReuse
	}
	if n, err := s.rdb.Exists(ctx, revokedKey(family)).Result(); err != nil || n > 0 {
		return "", "", ErrInvalidToken
	}
	return userID, family, nil
}

func (s *TokenStore) RevokeFamily(ctx context.Context, family string) error {
	return s.rdb.Set(ctx, revokedKey(family), "1", s.ttl).Err()
}

// RevokeByToken logs out the session a token belongs to. Unknown tokens are ignored.
func (s *TokenStore) RevokeByToken(ctx context.Context, token string) error {
	v, err := s.rdb.Get(ctx, s.tokenKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	} else if err != nil {
		return err
	}
	v = strings.TrimPrefix(v, "used:")
	if _, family, ok := strings.Cut(v, "|"); ok {
		return s.RevokeFamily(ctx, family)
	}
	return nil
}

// RevokeAllForUser ends every session of a user (password reset, ban, erasure).
func (s *TokenStore) RevokeAllForUser(ctx context.Context, userID string) error {
	// Void access tokens issued before now: they would otherwise keep working
	// until they expire, which for a ban or a stolen device is too long.
	if err := s.rdb.Set(ctx, cutoffKey(userID), time.Now().Unix(), cutoffTTL).Err(); err != nil {
		return err
	}
	fams, err := s.rdb.SMembers(ctx, userFamiliesKey(userID)).Result()
	if err != nil {
		return err
	}
	for _, f := range fams {
		if err := s.RevokeFamily(ctx, f); err != nil {
			return err
		}
	}
	return nil
}
