package users

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// MaxVoiceBytes bounds a voice intro upload (30 s of speech is far smaller).
	MaxVoiceBytes = 1 << 20
	// MaxVoiceSeconds is the longest voice intro.
	MaxVoiceSeconds = 30
)

// sniffAudio identifies the container from its leading bytes. The client's
// claimed type is never trusted, and only these three formats are accepted.
func sniffAudio(b []byte) (string, bool) {
	switch {
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "audio/mp4", true
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "audio/webm", true
	case len(b) >= 4 && string(b[:4]) == "OggS":
		return "audio/ogg", true
	}
	return "", false
}

// SetVoiceIntro stores (or replaces) the caller's voice intro.
func (s *Service) SetVoiceIntro(ctx context.Context, userID string, data []byte, seconds int) error {
	if seconds < 1 || seconds > MaxVoiceSeconds {
		return ValidationError{"a voice intro is 1 to 30 seconds"}
	}
	if len(data) == 0 || len(data) > MaxVoiceBytes {
		return ValidationError{"the recording is too large"}
	}
	mime, ok := sniffAudio(data)
	if !ok {
		return ValidationError{"unsupported audio format"}
	}
	id := uuid.NewString()
	if err := s.media.Put(ctx, id, data); err != nil {
		return err
	}
	var old *string
	err := s.db.QueryRow(ctx, `
		WITH prev AS (SELECT voice_id::text AS vid FROM profiles WHERE user_id=$1 FOR UPDATE)
		UPDATE profiles SET voice_id=$2::uuid, voice_seconds=$3, voice_mime=$4
		 WHERE user_id=$1 RETURNING (SELECT vid FROM prev)`, userID, id, seconds, mime).Scan(&old)
	if err != nil {
		_ = s.media.Delete(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if old != nil {
		_ = s.media.Delete(ctx, *old)
	}
	return nil
}

// DeleteVoiceIntro removes the caller's voice intro (idempotent).
func (s *Service) DeleteVoiceIntro(ctx context.Context, userID string) error {
	var old *string
	err := s.db.QueryRow(ctx, `
		WITH prev AS (SELECT voice_id::text AS vid FROM profiles WHERE user_id=$1 FOR UPDATE)
		UPDATE profiles SET voice_id=NULL, voice_seconds=NULL, voice_mime=NULL
		 WHERE user_id=$1 RETURNING (SELECT vid FROM prev)`, userID).Scan(&old)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if old != nil {
		return s.media.Delete(ctx, *old)
	}
	return nil
}

// OpenVoiceIntro returns the audio for a profile ref if (and only if) the
// viewer may see that person's full profile. Everything else is "not found",
// so an anonymous person's voice clip is not even confirmable.
func (s *Service) OpenVoiceIntro(ctx context.Context, viewerID, ref string) ([]byte, string, error) {
	if _, err := uuid.Parse(ref); err != nil {
		return nil, "", ErrNotFound
	}
	var ownerID, voiceID, mime string
	var mode Mode
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text, p.mode, p.voice_id::text, p.voice_mime
		  FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.public_ref = $1::uuid AND p.voice_id IS NOT NULL AND u.is_verified AND NOT u.is_banned
		   AND `+notBlocked("p.user_id", "$2::uuid"), ref, viewerID).Scan(&ownerID, &mode, &voiceID, &mime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	rel, err := s.relation(ctx, viewerID, ownerID)
	if err != nil {
		return nil, "", err
	}
	if ResolveVisibility(mode, rel) != VisFull {
		return nil, "", ErrNotFound
	}
	data, err := s.media.Get(ctx, voiceID)
	return data, mime, err
}

// attachVoice sets VoiceIntroS on the non-anonymous profiles in ps that have
// a voice intro. Anonymous profiles are never touched, so a clip cannot leak
// through the list views.
func (s *Service) attachVoice(ctx context.Context, ps []PublicProfile) {
	var refs []string
	idx := map[string][]int{}
	for i, p := range ps {
		if !p.Anonymous {
			refs = append(refs, p.Ref)
			idx[p.Ref] = append(idx[p.Ref], i)
		}
	}
	if len(refs) == 0 {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT public_ref::text, voice_seconds FROM profiles
		 WHERE public_ref = ANY($1::uuid[]) AND voice_id IS NOT NULL`, refs)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ref string
			var secs int
			if rows.Scan(&ref, &secs) == nil {
				for _, i := range idx[ref] {
					ps[i].VoiceIntroS = secs
				}
			}
		}
	}
	// Profile reels ride along: same rule, same list views. Failures here must
	// never break a profile list either.
	reels, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, v.seconds FROM videos v JOIN profiles p ON p.user_id = v.owner_id
		 WHERE v.kind = 'reel' AND v.status = 'live' AND p.public_ref = ANY($1::uuid[])`, refs)
	if err != nil {
		return
	}
	defer reels.Close()
	for reels.Next() {
		var ref string
		var secs int
		if reels.Scan(&ref, &secs) == nil {
			for _, i := range idx[ref] {
				ps[i].ReelS = secs
			}
		}
	}
	reels.Close()
	chs, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, COUNT(*) FROM profile_chapters c JOIN profiles p ON p.user_id = c.user_id
		 WHERE p.public_ref = ANY($1::uuid[]) GROUP BY p.public_ref`, refs)
	if err != nil {
		return
	}
	defer chs.Close()
	for chs.Next() {
		var ref string
		var n int
		if chs.Scan(&ref, &n) == nil {
			for _, i := range idx[ref] {
				ps[i].Chapters = n
			}
		}
	}
}

// PurgeVoiceFile deletes a user's voice clip file (account erasure; idempotent).
func (s *Service) PurgeVoiceFile(ctx context.Context, userID string) error {
	var id *string
	err := s.db.QueryRow(ctx, `SELECT voice_id::text FROM profiles WHERE user_id=$1::uuid`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || id == nil {
		return nil
	}
	if err != nil {
		return err
	}
	return s.media.Delete(ctx, *id)
}

// OpenVoiceOf returns a member's voice intro audio with no visibility check.
// Only callers that have already established both people's consent (the
// staged reveal in matching) may use it.
func (s *Service) OpenVoiceOf(ctx context.Context, ownerID string) ([]byte, string, error) {
	var voiceID, mime string
	err := s.db.QueryRow(ctx, `
		SELECT p.voice_id::text, p.voice_mime FROM profiles p JOIN users u ON u.id = p.user_id
		 WHERE p.user_id = $1::uuid AND p.voice_id IS NOT NULL AND u.is_verified AND NOT u.is_banned`, ownerID).Scan(&voiceID, &mime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	data, err := s.media.Get(ctx, voiceID)
	return data, mime, err
}
