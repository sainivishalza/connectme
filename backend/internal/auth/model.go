// Package auth implements registration, login, token rotation, password
// reset, RBAC and identity verification.
package auth

import (
	"errors"
	"time"
)

type RegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DateOfBirth string `json:"date_of_birth"` // YYYY-MM-DD, must be 18+
	Phone       string `json:"phone,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

// User is the internal account record. It is never serialized to clients.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	IsVerified   bool
	IsBanned     bool
	CreatedAt    time.Time
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type LoginResponse struct {
	TokenPair
	EmailVerified bool `json:"email_verified"`
}

// ValidationError carries a message that is safe to show to clients.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTooManyAttempts    = errors.New("too many failed attempts")
)
