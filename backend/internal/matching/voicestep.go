package matching

import (
	"context"
	"errors"

	"connectme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// VoiceStep is where an anonymous match stands on the voice step of the
// staged reveal. Seconds is the other person's voice intro length, shown only
// once the voice is unlocked.
type VoiceStep struct {
	ByMe     bool `json:"by_me"`
	Unlocked bool `json:"unlocked"`
	Seconds  int  `json:"seconds,omitempty"`
}

var ErrNeedsVoice = users.ValidationError{Msg: "record a voice intro on your profile first"}

// participant loads an active match the caller is in and who the other person is.
func (s *Service) participant(ctx context.Context, userID, matchID string) (other string, err error) {
	if _, perr := uuid.Parse(matchID); perr != nil {
		return "", ErrNotFound
	}
	err = s.db.QueryRow(ctx, `
		SELECT CASE WHEN user1_id=$2::uuid THEN user2_id ELSE user1_id END::text
		  FROM matches WHERE id=$1::uuid AND (user1_id=$2::uuid OR user2_id=$2::uuid) AND status='matched'`,
		matchID, userID).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return other, err
}

// RevealVoice records the caller's wish to swap voice intros with this match.
// The caller needs a voice intro of their own (it is a swap). The response
// says only whether the voice is now unlocked, not whether the other side was
// waiting.
func (s *Service) RevealVoice(ctx context.Context, userID, matchID string) (*VoiceStep, error) {
	if _, err := s.participant(ctx, userID, matchID); err != nil {
		return nil, err
	}
	var has bool
	if err := s.db.QueryRow(ctx, `SELECT voice_id IS NOT NULL FROM profiles WHERE user_id=$1::uuid`, userID).Scan(&has); err != nil || !has {
		return nil, ErrNeedsVoice
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO voice_consents (match_id, user_id) VALUES ($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, matchID, userID); err != nil {
		return nil, err
	}
	steps, err := s.voiceSteps(ctx, userID, map[string]string{matchID: ""})
	if err != nil {
		return nil, err
	}
	st := steps[matchID]
	st.Seconds = 0
	return &st, nil
}

// voiceSteps computes the voice step for matches (matchID -> other user id).
func (s *Service) voiceSteps(ctx context.Context, userID string, others map[string]string) (map[string]VoiceStep, error) {
	ids := make([]string, 0, len(others))
	for id := range others {
		ids = append(ids, id)
	}
	rows, err := s.db.Query(ctx, `
		SELECT m.id::text,
		       EXISTS (SELECT 1 FROM voice_consents v WHERE v.match_id = m.id AND v.user_id = $1::uuid),
		       (SELECT COUNT(*) FROM voice_consents v WHERE v.match_id = m.id) >= 2,
		       CASE WHEN m.user1_id = $1::uuid THEN p2.voice_seconds ELSE p1.voice_seconds END
		  FROM matches m
		  JOIN profiles p1 ON p1.user_id = m.user1_id
		  JOIN profiles p2 ON p2.user_id = m.user2_id
		 WHERE m.id = ANY($2::uuid[])`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]VoiceStep{}
	for rows.Next() {
		var id string
		var st VoiceStep
		var secs *int
		if err := rows.Scan(&id, &st.ByMe, &st.Unlocked, &secs); err != nil {
			return nil, err
		}
		if st.Unlocked && secs != nil {
			st.Seconds = *secs
		}
		out[id] = st
	}
	return out, rows.Err()
}

// MatchVoice returns the other person's voice intro once both have agreed.
func (s *Service) MatchVoice(ctx context.Context, userID, matchID string) ([]byte, string, error) {
	other, err := s.participant(ctx, userID, matchID)
	if err != nil {
		return nil, "", err
	}
	var both bool
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) >= 2 FROM voice_consents WHERE match_id=$1::uuid`, matchID).Scan(&both); err != nil {
		return nil, "", err
	}
	if !both {
		return nil, "", ErrNotFound
	}
	var blocked bool
	if err := s.db.QueryRow(ctx, `SELECT NOT `+users.NotBlockedSQL("$1::uuid", "$2::uuid"), userID, other).Scan(&blocked); err != nil || blocked {
		return nil, "", ErrNotFound
	}
	return s.users.OpenVoiceOf(ctx, other)
}
