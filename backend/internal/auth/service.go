package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"connectme/pkg/metrics"
	"connectme/pkg/middleware"
	"connectme/pkg/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost       = 12
	maxLoginFailures = 10
	loginFailWindow  = 15 * time.Minute
	verifyTokenTTL   = 24 * time.Hour
	resetTokenTTL    = time.Hour
	pgUniqueViolated = "23505"
)

// dummyHash lets Login spend the same bcrypt time for unknown emails,
// so response timing does not reveal which accounts exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("connectme-timing-equalizer"), bcryptCost)

type Service struct {
	db        *pgxpool.Pool
	rdb       *redis.Client
	tokens    *TokenStore
	mailer    Mailer
	secret    []byte
	accessTTL time.Duration
	now       func() time.Time
}

func NewService(db *pgxpool.Pool, rdb *redis.Client, tokens *TokenStore, m Mailer, secret []byte, accessTTL time.Duration) *Service {
	return &Service{db: db, rdb: rdb, tokens: tokens, mailer: m, secret: secret, accessTTL: accessTTL, now: time.Now}
}

// Register creates an account. A duplicate email returns nil (and emails the
// owner) so the endpoint cannot be used to enumerate accounts.
func (s *Service) Register(ctx context.Context, req RegisterRequest) error {
	email, err := utils.NormalizeEmail(req.Email)
	if err != nil {
		return ValidationError{err.Error()}
	}
	if err := utils.ValidatePassword(req.Password); err != nil {
		return ValidationError{err.Error()}
	}
	dob, err := utils.ParseAdultDOB(req.DateOfBirth, utils.AgeGateReference(s.now()))
	if err != nil {
		return ValidationError{err.Error()}
	}
	var phone *string
	if req.Phone != "" {
		p, err := utils.NormalizePhone(req.Phone)
		if err != nil {
			return ValidationError{err.Error()}
		}
		phone = &p
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		return err
	}
	if s.identityBanned(ctx, email) {
		return nil // same response as a duplicate: no signal to the caller
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var userID string
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, phone, password_hash, date_of_birth) VALUES ($1,$2,$3,$4) RETURNING id`,
		email, phone, string(hash), dob).Scan(&userID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolated {
		_ = s.mailer.SendAlreadyRegistered(ctx, email)
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO profiles (user_id) VALUES ($1)`, userID); err != nil {
		return err
	}
	token, err := s.insertEmailToken(ctx, tx, userID, "verify_email", verifyTokenTTL)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	metrics.Event("signup")
	_ = s.mailer.SendVerification(ctx, email, token) // delivery failure must not fail signup
	return nil
}

// Login verifies credentials and issues a token pair. All failure causes
// (unknown email, bad password, banned) return the same error.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	email, emailErr := utils.NormalizeEmail(req.Email)
	failKey := "loginfail:" + hashToken(email)

	if emailErr == nil {
		if n, _ := s.rdb.Get(ctx, failKey).Int(); n >= maxLoginFailures {
			return nil, ErrTooManyAttempts
		}
	}
	var u User
	err := pgx.ErrNoRows
	if emailErr == nil {
		err = s.db.QueryRow(ctx,
			`SELECT id, password_hash, role, is_verified, is_banned FROM users WHERE LOWER(email)=$1`, email).
			Scan(&u.ID, &u.PasswordHash, &u.Role, &u.IsVerified, &u.IsBanned)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		s.noteFailure(ctx, failKey, emailErr)
		metrics.Event("login_failed")
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil || u.IsBanned {
		s.noteFailure(ctx, failKey, nil)
		metrics.Event("login_failed")
		return nil, ErrInvalidCredentials
	}
	_ = s.rdb.Del(ctx, failKey).Err()
	metrics.Event("login_ok")

	pair, err := s.issuePair(ctx, u.ID, u.Role, "")
	if err != nil {
		return nil, err
	}
	return &LoginResponse{TokenPair: *pair, EmailVerified: u.IsVerified}, nil
}

func (s *Service) noteFailure(ctx context.Context, key string, emailErr error) {
	if emailErr != nil {
		return
	}
	if n, err := s.rdb.Incr(ctx, key).Result(); err == nil && n == 1 {
		s.rdb.Expire(ctx, key, loginFailWindow)
	}
}

// Refresh rotates a refresh token. Role and ban status are re-read from the
// database so changes take effect within one access-token lifetime.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	userID, family, err := s.tokens.Rotate(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	var role string
	var banned bool
	err = s.db.QueryRow(ctx, `SELECT role, is_banned FROM users WHERE id=$1`, userID).Scan(&role, &banned)
	if errors.Is(err, pgx.ErrNoRows) || banned {
		_ = s.tokens.RevokeFamily(ctx, family)
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	return s.issuePair(ctx, userID, role, family)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.tokens.RevokeByToken(ctx, refreshToken)
}

// ForgotPassword always succeeds from the caller's point of view.
func (s *Service) ForgotPassword(ctx context.Context, rawEmail string) error {
	email, err := utils.NormalizeEmail(rawEmail)
	if err != nil {
		return nil
	}
	var id string
	err = s.db.QueryRow(ctx, `SELECT id FROM users WHERE LOWER(email)=$1 AND NOT is_banned`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := s.insertEmailToken(ctx, s.db, id, "reset_password", resetTokenTTL)
	if err != nil {
		return err
	}
	_ = s.mailer.SendPasswordReset(ctx, email, token)
	return nil
}

// ResetPassword consumes a reset token, sets the new password and signs out
// every existing session.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := utils.ValidatePassword(newPassword); err != nil {
		return ValidationError{err.Error()}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	userID, err := s.consumeEmailToken(ctx, token, "reset_password")
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx,
		`UPDATE users SET password_hash=$1, updated_at=NOW() WHERE id=$2`, string(hash), userID); err != nil {
		return err
	}
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return s.dropPushDevices(ctx, userID)
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, err := s.consumeEmailToken(ctx, token, "verify_email")
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET is_verified=TRUE, updated_at=NOW() WHERE id=$1`, userID)
	return err
}

func (s *Service) issuePair(ctx context.Context, userID, role, family string) (*TokenPair, error) {
	access, err := middleware.SignAccessToken(s.secret, userID, role, s.accessTTL)
	if err != nil {
		return nil, err
	}
	refresh, _, err := s.tokens.Issue(ctx, userID, family)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer",
		ExpiresIn: int(s.accessTTL.Seconds())}, nil
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// insertEmailToken stores only the SHA-256 of a random token; the raw token is
// returned for emailing. Outstanding tokens of the same purpose are voided.
func (s *Service) insertEmailToken(ctx context.Context, q execer, userID, purpose string, ttl time.Duration) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	sum := sha256.Sum256([]byte(token))
	if _, err := q.Exec(ctx,
		`UPDATE email_tokens SET used_at=NOW() WHERE user_id=$1 AND purpose=$2 AND used_at IS NULL`,
		userID, purpose); err != nil {
		return "", err
	}
	_, err := q.Exec(ctx,
		`INSERT INTO email_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1,$2,$3,$4)`,
		userID, purpose, sum[:], s.now().Add(ttl))
	if err != nil {
		return "", fmt.Errorf("store email token: %w", err)
	}
	return token, nil
}

// consumeEmailToken atomically marks a valid token as used and returns its owner.
func (s *Service) consumeEmailToken(ctx context.Context, token, purpose string) (string, error) {
	sum := sha256.Sum256([]byte(token))
	var userID string
	err := s.db.QueryRow(ctx,
		`UPDATE email_tokens SET used_at=NOW()
		  WHERE token_hash=$1 AND purpose=$2 AND used_at IS NULL AND expires_at > NOW()
		  RETURNING user_id`, sum[:], purpose).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidToken
	}
	return userID, err
}

// identityBanned reports whether this email belongs to an erased, banned account.
func (s *Service) identityBanned(ctx context.Context, email string) bool {
	sum := sha256.Sum256([]byte(email))
	var banned bool
	if err := s.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM banned_identities WHERE email_hash=$1)`, sum[:]).Scan(&banned); err != nil {
		return false
	}
	return banned
}
