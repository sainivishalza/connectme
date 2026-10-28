// Package videos implements short videos (the video feed) and video profile
// reels.
//
// Privacy rules, enforced here and nowhere else:
//   - a short can only be posted by an Open-mode member, because a face and a
//     voice identify a person;
//   - a reel is visible exactly when the owner's photos are
//     (users.Service.SeesFull), so Incognito and Hotel profiles never leak one;
//   - files are served only through short-lived signed URLs, and only after
//     the same access check.
//
// Launch note: there is no video-aware CSAM/abuse scanner behind this feature.
// It is off unless ENABLE_VIDEO=true, and must stay off in production until a
// provider is wired in (docs/AUDIT.md).
package videos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"connectme/internal/push"
	"connectme/internal/sounds"
	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	ErrNotOpen  = errors.New("only verified members in Open mode can post videos")
)

const (
	MaxBytes     = 12 << 20
	MaxSeconds   = 30
	shortsPerDay = 5
	ticketTTL    = 10 * time.Minute
	pageSize     = 15
)

// TextVetter screens captions; *users.Service satisfies it.
type TextVetter interface {
	VetText(ctx context.Context, userID, kind, text string) error
}

type Service struct {
	db       *pgxpool.Pool
	media    users.MediaStore
	users    *users.Service
	vetter   TextVetter
	scanner  Scanner
	notifier push.Notifier
	secret   []byte
	now      func() time.Time
}

// SetScanner replaces the abuse scanner (the default is the allow-all stub).
func (s *Service) SetScanner(sc Scanner) { s.scanner = sc }

func NewService(db *pgxpool.Pool, media users.MediaStore, u *users.Service, secret []byte) *Service {
	return &Service{db: db, media: media, users: u, vetter: u, scanner: StubScanner{}, secret: secret, now: time.Now}
}

type Author struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
}

// Video is the public view of a clip.
type Video struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Caption   string    `json:"caption"`
	Seconds   int       `json:"seconds"`
	CreatedAt time.Time `json:"created_at"`
	Likes     int       `json:"likes"`
	Liked     bool      `json:"liked"`
	Mine      bool      `json:"mine"`
	Comments  int       `json:"comments"`
	Sound     string    `json:"sound,omitempty"` // id of the background loop, if any
	SoundName string    `json:"sound_name,omitempty"`
	Following bool      `json:"following,omitempty"` // the viewer follows the author
	Status    string    `json:"status"`              // live | held (waiting for a moderator; only the owner sees it)
	Author    *Author   `json:"author,omitempty"`    // shorts only: their authors are Open-mode members
}

const videoCols = `
	v.id::text, v.kind, v.caption, v.seconds, v.created_at,
	(SELECT COUNT(*) FROM video_likes l WHERE l.video_id = v.id),
	EXISTS (SELECT 1 FROM video_likes l WHERE l.video_id = v.id AND l.user_id = $1::uuid),
	v.owner_id = $1::uuid, v.status, p.public_ref::text, p.display_name,
	(SELECT COUNT(*) FROM video_comments vc WHERE vc.video_id = v.id), v.sound_id,
	EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = $1::uuid AND f.followee_id = v.owner_id)`

func scanVideo(row pgx.Row) (*Video, error) {
	var v Video
	var ref, name string
	if err := row.Scan(&v.ID, &v.Kind, &v.Caption, &v.Seconds, &v.CreatedAt, &v.Likes, &v.Liked, &v.Mine, &v.Status, &ref, &name, &v.Comments, &v.Sound, &v.Following); err != nil {
		return nil, err
	}
	if v.Kind == "short" {
		v.Author = &Author{Ref: ref, Name: name}
	}
	v.SoundName = sounds.Name(v.Sound)
	return &v, nil
}

func parse(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	return nil
}

// Upload stores a clip. For a reel it replaces the previous one.
func (s *Service) Upload(ctx context.Context, userID, kind, caption string, data []byte) (*Video, error) {
	return s.UploadWithSound(ctx, userID, kind, caption, "", data)
}

// UploadWithSound is Upload with an optional sound from the built-in library
// (shorts only).
func (s *Service) UploadWithSound(ctx context.Context, userID, kind, caption, sound string, data []byte) (*Video, error) {
	if sound != "" && (kind != "short" || !sounds.Exists(sound)) {
		return nil, users.ValidationError{Msg: "unknown sound"}
	}
	if kind != "short" && kind != "reel" {
		return nil, users.ValidationError{Msg: "kind must be short or reel"}
	}
	caption = strings.TrimSpace(caption)
	if utf8.RuneCountInString(caption) > 150 {
		return nil, users.ValidationError{Msg: "caption can be up to 150 characters"}
	}
	for _, c := range caption {
		if unicode.IsControl(c) {
			return nil, users.ValidationError{Msg: "caption contains invalid characters"}
		}
	}
	var ok bool
	var mode string
	if err := s.db.QueryRow(ctx, `
		SELECT u.is_verified AND NOT u.is_banned, p.mode FROM users u JOIN profiles p ON p.user_id=u.id WHERE u.id=$1::uuid`,
		userID).Scan(&ok, &mode); err != nil || !ok {
		return nil, ErrNotOpen
	}
	if kind == "short" && mode != "open" {
		return nil, ErrNotOpen
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, users.ValidationError{Msg: "the video is too large (12 MB at most)"}
	}
	mime, okType := sniff(data)
	if !okType {
		return nil, users.ValidationError{Msg: "unsupported video format; use MP4 or MOV"}
	}
	dur, okDur := duration(data)
	if !okDur || dur < 0.5 {
		return nil, users.ValidationError{Msg: "this video file cannot be read"}
	}
	if dur > MaxSeconds+0.5 {
		return nil, users.ValidationError{Msg: "videos are 30 seconds at most"}
	}
	seconds := int(math.Ceil(dur))
	if seconds > MaxSeconds {
		seconds = MaxSeconds
	}
	if caption != "" && s.vetter != nil {
		if err := s.vetter.VetText(ctx, userID, "post", caption); err != nil {
			return nil, err
		}
	}
	if kind == "short" {
		var n int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM videos WHERE owner_id=$1::uuid AND kind='short' AND created_at > $2`,
			userID, s.now().Add(-24*time.Hour)).Scan(&n); err != nil {
			return nil, err
		}
		if n >= shortsPerDay {
			return nil, users.ValidationError{Msg: "you can post 5 videos a day; try again tomorrow"}
		}
	}
	status, note := "live", ""
	verdict, scanErr := s.scanner.Scan(ctx, data, mime)
	switch {
	case scanErr != nil:
		slog.Warn("videos: scanner unavailable, holding clip for review", "err", scanErr)
		status, note = "held", "scanner unavailable"
	case verdict.Action == "block":
		s.autoReport(ctx, userID, "video blocked by scanner: "+verdict.Reason)
		return nil, users.ValidationError{Msg: "This video is not allowed."}
	case verdict.Action == "flag":
		status, note = "held", verdict.Reason
	}
	blob := uuid.NewString()
	if err := s.media.Put(ctx, blob, data); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		_ = s.media.Delete(ctx, blob)
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var oldBlob *string
	if kind == "reel" {
		err := tx.QueryRow(ctx, `DELETE FROM videos WHERE owner_id=$1::uuid AND kind='reel' RETURNING blob_id::text`, userID).Scan(&oldBlob)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			_ = s.media.Delete(ctx, blob)
			return nil, err
		}
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO videos (owner_id, kind, blob_id, mime, seconds, bytes, caption, status, scan_note, scanner, sound_id)
		VALUES ($1::uuid,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`,
		userID, kind, blob, mime, seconds, len(data), caption, status, note, s.scanner.Name(), sound).Scan(&id); err != nil {
		_ = s.media.Delete(ctx, blob)
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = s.media.Delete(ctx, blob)
		return nil, err
	}
	if oldBlob != nil {
		_ = s.media.Delete(ctx, *oldBlob)
	}
	return s.get(ctx, userID, id)
}

func (s *Service) get(ctx context.Context, viewer, id string) (*Video, error) {
	return scanVideo(s.db.QueryRow(ctx, `SELECT `+videoCols+` FROM videos v JOIN profiles p ON p.user_id=v.owner_id WHERE v.id=$2::uuid`, viewer, id))
}

// Feed returns recent shorts from Open-mode members the viewer may see.
func (s *Service) Feed(ctx context.Context, viewer string, before time.Time, followingOnly bool) ([]Video, error) {
	if before.IsZero() {
		before = s.now().Add(time.Minute)
	}
	rows, err := s.db.Query(ctx, `SELECT `+videoCols+`
		  FROM videos v
		  JOIN profiles p ON p.user_id = v.owner_id AND p.mode = 'open'
		  JOIN users u ON u.id = v.owner_id AND u.is_verified AND NOT u.is_banned
		 WHERE v.kind = 'short' AND v.status = 'live' AND v.created_at < $2
		   AND (NOT $4::boolean OR EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = $1::uuid AND f.followee_id = v.owner_id))
		   AND `+users.NotBlockedSQL("v.owner_id", "$1::uuid")+`
		   AND EXISTS (SELECT 1 FROM users me WHERE me.id = $1::uuid AND me.is_verified AND NOT me.is_banned)
		 ORDER BY v.created_at DESC LIMIT $3`, viewer, before, pageSize, followingOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Video{}
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// Mine lists the caller's own clips.
func (s *Service) Mine(ctx context.Context, userID string) ([]Video, error) {
	rows, err := s.db.Query(ctx, `SELECT `+videoCols+` FROM videos v JOIN profiles p ON p.user_id=v.owner_id
		 WHERE v.owner_id=$1::uuid ORDER BY v.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Video{}
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// Reel returns the profile reel for a ref if the viewer may see that profile.
func (s *Service) Reel(ctx context.Context, viewer, ref string) (*Video, error) {
	if err := parse(ref); err != nil {
		return nil, err
	}
	var owner string
	if err := s.db.QueryRow(ctx, `SELECT user_id::text FROM profiles WHERE public_ref=$1::uuid`, ref).Scan(&owner); err != nil {
		return nil, ErrNotFound
	}
	if ok, err := s.users.SeesFull(ctx, viewer, owner); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	v, err := scanVideo(s.db.QueryRow(ctx, `SELECT `+videoCols+` FROM videos v JOIN profiles p ON p.user_id=v.owner_id
		 WHERE v.owner_id=$2::uuid AND v.kind='reel' AND (v.status='live' OR v.owner_id=$1::uuid)`, viewer, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

type access struct {
	owner, kind, blob, mime, status string
}

// check loads a clip and verifies the viewer may watch it.
func (s *Service) check(ctx context.Context, viewer, id string) (*access, error) {
	if err := parse(id); err != nil {
		return nil, err
	}
	var a access
	var ownerMode string
	var verified, banned bool
	err := s.db.QueryRow(ctx, `
		SELECT v.owner_id::text, v.kind, v.blob_id::text, v.mime, v.status, p.mode, u.is_verified, u.is_banned
		  FROM videos v JOIN profiles p ON p.user_id=v.owner_id JOIN users u ON u.id=v.owner_id
		 WHERE v.id=$1::uuid`, id).Scan(&a.owner, &a.kind, &a.blob, &a.mime, &a.status, &ownerMode, &verified, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if a.owner == viewer {
		return &a, nil
	}
	if !verified || banned || a.status != "live" {
		return nil, ErrNotFound
	}
	var me bool
	if err := s.db.QueryRow(ctx, `SELECT is_verified AND NOT is_banned FROM users WHERE id=$1::uuid`, viewer).Scan(&me); err != nil || !me {
		return nil, ErrNotFound
	}
	if a.kind == "short" {
		var blocked bool
		if err := s.db.QueryRow(ctx, `SELECT NOT `+users.NotBlockedSQL("$1::uuid", "$2::uuid"), a.owner, viewer).Scan(&blocked); err != nil {
			return nil, err
		}
		if ownerMode != "open" || blocked {
			return nil, ErrNotFound
		}
		return &a, nil
	}
	if ok, err := s.users.SeesFull(ctx, viewer, a.owner); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	return &a, nil
}

func (s *Service) sign(id string, exp int64) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("video|" + id + "|" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(m.Sum(nil))
}

// Ticket returns a short-lived signed path the player can fetch without
// custom headers (browsers and native players cannot send them).
func (s *Service) Ticket(ctx context.Context, viewer, id string) (string, error) {
	if _, err := s.check(ctx, viewer, id); err != nil {
		return "", err
	}
	exp := s.now().Add(ticketTTL).Unix()
	return fmt.Sprintf("/videos/file/%s?exp=%d&sig=%s", id, exp, s.sign(id, exp)), nil
}

// Open returns the bytes for a valid, unexpired ticket.
func (s *Service) Open(ctx context.Context, id, expStr, sig string) ([]byte, string, error) {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || exp < s.now().Unix() || !hmac.Equal([]byte(sig), []byte(s.sign(id, exp))) {
		return nil, "", ErrNotFound
	}
	if err := parse(id); err != nil {
		return nil, "", err
	}
	var blob, mime string
	var banned bool
	err = s.db.QueryRow(ctx, `SELECT v.blob_id::text, v.mime, u.is_banned FROM videos v JOIN users u ON u.id=v.owner_id WHERE v.id=$1::uuid`, id).
		Scan(&blob, &mime, &banned)
	if err != nil || banned {
		return nil, "", ErrNotFound
	}
	data, err := s.media.Get(ctx, blob)
	return data, mime, err
}

// Like toggles the caller's like on a short.
func (s *Service) Like(ctx context.Context, userID, id string) (liked bool, likes int, err error) {
	a, err := s.check(ctx, userID, id)
	if err != nil {
		return false, 0, err
	}
	if a.kind != "short" {
		return false, 0, ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `DELETE FROM video_likes WHERE video_id=$1::uuid AND user_id=$2::uuid`, id, userID)
	if err != nil {
		return false, 0, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.db.Exec(ctx, `INSERT INTO video_likes (video_id, user_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, id, userID); err != nil {
			return false, 0, err
		}
		liked = true
	}
	err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM video_likes WHERE video_id=$1::uuid`, id).Scan(&likes)
	return liked, likes, err
}

// Report files a report against a clip the caller can watch.
func (s *Service) Report(ctx context.Context, userID, id, reason string) error {
	switch reason {
	case "spam", "harassment", "inappropriate_content", "scam", "underage", "other":
	default:
		return users.ValidationError{Msg: "unknown reason"}
	}
	a, err := s.check(ctx, userID, id)
	if err != nil {
		return err
	}
	if a.owner == userID {
		return ErrNotFound
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		SELECT $1::uuid, v.owner_id, $3, 'video ' || v.kind, jsonb_build_array(v.id::text, v.caption)
		  FROM videos v WHERE v.id=$2::uuid`, userID, id, reason)
	return err
}

// Delete removes the caller's own clip and its file.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	if err := parse(id); err != nil {
		return err
	}
	var blob string
	err := s.db.QueryRow(ctx, `DELETE FROM videos WHERE id=$1::uuid AND owner_id=$2::uuid RETURNING blob_id::text`, id, userID).Scan(&blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.media.Delete(ctx, blob)
}

// StaffRemove deletes any clip for moderation; the caller audits it.
func (s *Service) StaffRemove(ctx context.Context, id string) (author, kind string, err error) {
	if err := parse(id); err != nil {
		return "", "", err
	}
	var blob string
	err = s.db.QueryRow(ctx, `DELETE FROM videos WHERE id=$1::uuid RETURNING owner_id::text, kind, blob_id::text`, id).Scan(&author, &kind, &blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	_ = s.media.Delete(ctx, blob)
	return author, kind, nil
}

// PurgeFiles deletes every file a user uploaded (account erasure; idempotent).
func (s *Service) PurgeFiles(ctx context.Context, userID string) error {
	rows, err := s.db.Query(ctx, `SELECT blob_id::text FROM videos WHERE owner_id=$1::uuid`, userID)
	if err != nil {
		return err
	}
	var blobs []string
	for rows.Next() {
		var b string
		if rows.Scan(&b) == nil {
			blobs = append(blobs, b)
		}
	}
	rows.Close()
	for _, b := range blobs {
		if err := s.media.Delete(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

// autoReport puts a blocked upload in the moderators' queue as an automatic flag.
func (s *Service) autoReport(ctx context.Context, userID, detail string) {
	ev, _ := json.Marshal([]string{detail})
	if _, err := s.db.Exec(ctx, `INSERT INTO reports (reporter_id, reported_id, reason, details, evidence)
		VALUES (NULL, $1::uuid, 'auto_review', $3, $2)`, userID, ev, "video: "+detail); err != nil {
		slog.Error("videos: could not queue blocked upload", "err", err)
	}
}

// HeldVideo is a clip waiting for a moderator.
type HeldVideo struct {
	ID        string    `json:"id"`
	OwnerRef  string    `json:"owner_ref"`
	Kind      string    `json:"kind"`
	Caption   string    `json:"caption"`
	Seconds   int       `json:"seconds"`
	Note      string    `json:"note"`
	Scanner   string    `json:"scanner"`
	CreatedAt time.Time `json:"created_at"`
}

// Held lists clips waiting for review, oldest first.
func (s *Service) Held(ctx context.Context) ([]HeldVideo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT v.id::text, p.public_ref::text, v.kind, v.caption, v.seconds, v.scan_note, v.scanner, v.created_at
		  FROM videos v JOIN profiles p ON p.user_id = v.owner_id
		 WHERE v.status = 'held' ORDER BY v.created_at LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HeldVideo{}
	for rows.Next() {
		var h HeldVideo
		if err := rows.Scan(&h.ID, &h.OwnerRef, &h.Kind, &h.Caption, &h.Seconds, &h.Note, &h.Scanner, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Approve publishes a held clip; the reviewer is recorded.
func (s *Service) Approve(ctx context.Context, id, reviewerID string) (author string, err error) {
	if err := parse(id); err != nil {
		return "", err
	}
	err = s.db.QueryRow(ctx, `UPDATE videos SET status='live', reviewed_by=$2::uuid, reviewed_at=NOW()
		 WHERE id=$1::uuid AND status='held' RETURNING owner_id::text`, id, reviewerID).Scan(&author)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return author, err
}

// StaffTicket mints a signed URL so a moderator can watch any clip, held or
// not. The caller must audit the view.
func (s *Service) StaffTicket(ctx context.Context, id string) (path, author string, err error) {
	if err := parse(id); err != nil {
		return "", "", err
	}
	if err := s.db.QueryRow(ctx, `SELECT owner_id::text FROM videos WHERE id=$1::uuid`, id).Scan(&author); err != nil {
		return "", "", ErrNotFound
	}
	exp := s.now().Add(ticketTTL).Unix()
	return fmt.Sprintf("/videos/file/%s?exp=%d&sig=%s", id, exp, s.sign(id, exp)), author, nil
}
