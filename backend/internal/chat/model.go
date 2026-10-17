// Package chat stores end-to-end encrypted messages and distributes Signal
// public key bundles. The server only ever sees ciphertext.
package chat

import (
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	MaxCiphertextBytes = 16 << 10
	MaxMediaBytes      = 8 << 20
	maxOneTimePrekeys  = 200
	messagesPerPage    = 50
)

type MessageType string

const (
	TypeText  MessageType = "text"
	TypeImage MessageType = "image"
	TypeVoice MessageType = "voice"
	// "gift" is added with payments (Step 7): it must debit coins first.
)

// allowedBurn are the burn-after-reading choices, in seconds (0 = keep).
var allowedBurn = map[int]bool{0: true, 5: true, 10: true, 30: true, 60: true, 300: true, 3600: true, 86400: true}

var (
	ErrNotFound = errors.New("not found")
	ErrTooLarge = errors.New("payload too large")
)

type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// messageDoc is the MongoDB document. Ciphertext is opaque to the server.
type messageDoc struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	MatchID    string        `bson:"match_id"`
	CheckinID  string        `bson:"checkin_id,omitempty"`
	SenderID   string        `bson:"sender_id"`
	Type       string        `bson:"type"`
	Ciphertext string        `bson:"ciphertext"`
	MediaID    string        `bson:"media_id,omitempty"`
	BurnAfterS int           `bson:"burn_after_s"`
	BurnAt     *time.Time    `bson:"burn_at,omitempty"`
	ReadAt     *time.Time    `bson:"read_at,omitempty"`
	CreatedAt  time.Time     `bson:"created_at"`
}

type mediaDoc struct {
	ID         string    `bson:"_id"`
	MatchID    string    `bson:"match_id"`
	CheckinID  string    `bson:"checkin_id,omitempty"`
	UploaderID string    `bson:"uploader_id"`
	Attached   bool      `bson:"attached"`
	CreatedAt  time.Time `bson:"created_at"`
}

// Message is the API shape. SenderID is never exposed; FromMe is per viewer.
type Message struct {
	ID         string      `json:"id"`
	MatchID    string      `json:"match_id"`
	Type       MessageType `json:"type"`
	Ciphertext string      `json:"ciphertext"`
	MediaID    string      `json:"media_id,omitempty"`
	BurnAfterS int         `json:"burn_after_s"`
	CreatedAt  time.Time   `json:"created_at"`
	ReadAt     *time.Time  `json:"read_at,omitempty"`
	FromMe     bool        `json:"from_me"`
	// SenderToken is a pseudonym for the sender that is stable within this
	// conversation only (see token()). Group clients use it to pick the right
	// sender key; it identifies nobody outside the conversation.
	SenderToken string `json:"sender_token"`
}

type SendRequest struct {
	Type       MessageType `json:"type"`
	Ciphertext string      `json:"ciphertext"` // base64 Signal message
	MediaID    string      `json:"media_id"`
	BurnAfterS int         `json:"burn_after_s"`
}

// KeyBundle is a user's public Signal keys, as uploaded and as fetched.
type KeyBundle struct {
	RegistrationID  int      `json:"registration_id"`
	IdentityKey     string   `json:"identity_key"`
	SignedPreKeyID  int      `json:"signed_prekey_id"`
	SignedPreKey    string   `json:"signed_prekey"`
	SignedPreKeySig string   `json:"signed_prekey_sig"`
	OneTimePreKeys  []PreKey `json:"one_time_prekeys,omitempty"` // upload only
	PreKey          *PreKey  `json:"prekey,omitempty"`           // fetch only (consumed)
}

type PreKey struct {
	KeyID     int    `json:"key_id"`
	PublicKey string `json:"public_key"`
}
