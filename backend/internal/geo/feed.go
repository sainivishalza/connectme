package geo

import (
	"context"
	"errors"

	"connectme/internal/users"
	"connectme/pkg/middleware"
	"connectme/pkg/utils"
)

// Feed lists other people currently checked in at the same hotel, as
// anonymous (hotel mode) or full (opted into open) cards. Only people who are
// themselves checked in there may look.
func (s *Service) Feed(ctx context.Context, userID, hotelID, mood string, limit, offset int) ([]users.PublicProfile, error) {
	if mood != "" && !validMood(mood) {
		return nil, users.ValidationError{Msg: "mood must be business, company or guide"}
	}
	_, hotelName, err := s.activeCheckin(ctx, userID, hotelID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT c.user_id::text, COALESCE(c.traveler_mood,'')
		  FROM hotel_checkins c JOIN users u ON u.id = c.user_id
		  JOIN profiles pr ON pr.user_id = c.user_id AND NOT pr.invisible
		 WHERE c.hotel_id = $1::uuid AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW()
		   AND c.user_id <> $2::uuid AND u.is_verified AND NOT u.is_banned
		   AND ($5 = '' OR c.traveler_mood = $5)
		   AND `+users.NotBlockedSQL("c.user_id", "$2::uuid")+`
		   AND NOT EXISTS (SELECT 1 FROM swipes sw WHERE sw.actor_id = $2::uuid AND sw.target_id = c.user_id)
		 ORDER BY c.checked_in_at DESC LIMIT $3 OFFSET $4`, hotelID, userID, limit, offset, mood)
	if err != nil {
		return nil, err
	}
	var ids []string
	moods := map[string]string{}
	for rows.Next() {
		var id, m string
		if err := rows.Scan(&id, &m); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		moods[id] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cards, err := s.users.Cards(ctx, userID, ids, hotelName)
	if err != nil {
		return nil, err
	}
	out := []users.PublicProfile{}
	for _, id := range ids {
		card, ok := cards[id]
		if !ok {
			continue
		}
		card.TravelMood = moods[id]
		out = append(out, card)
	}
	return out, nil
}

func validMood(m string) bool { return m == "business" || m == "company" || m == "guide" }

// SetMood records the caller's mood for their current stay (empty clears it).
func (s *Service) SetMood(ctx context.Context, userID, mood string) error {
	if mood != "" && !validMood(mood) {
		return users.ValidationError{Msg: "mood must be business, company or guide"}
	}
	tag, err := s.db.Exec(ctx, `UPDATE hotel_checkins SET traveler_mood = NULLIF($2,'') WHERE user_id=$1::uuid AND checked_out_at IS NULL`, userID, mood)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotCheckedIn
	}
	return nil
}

// Moods counts the guests at the caller's hotel per mood (others only).
func (s *Service) Moods(ctx context.Context, userID, hotelID string) (map[string]int, error) {
	if _, _, err := s.activeCheckin(ctx, userID, hotelID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT c.traveler_mood, COUNT(*)
		  FROM hotel_checkins c JOIN users u ON u.id = c.user_id
		  JOIN profiles pr ON pr.user_id = c.user_id AND NOT pr.invisible
		 WHERE c.hotel_id = $1::uuid AND c.checked_out_at IS NULL AND c.auto_delete_at > NOW()
		   AND c.user_id <> $2::uuid AND u.is_verified AND NOT u.is_banned AND c.traveler_mood IS NOT NULL
		   AND `+users.NotBlockedSQL("c.user_id", "$2::uuid")+`
		 GROUP BY c.traveler_mood`, hotelID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"business": 0, "company": 0, "guide": 0}
	for rows.Next() {
		var m string
		var n int
		if err := rows.Scan(&m, &n); err != nil {
			return nil, err
		}
		out[m] = n
	}
	return out, rows.Err()
}

// Board lists the hotel's "anyone want dinner?" posts from people checked in now.
func (s *Service) Board(ctx context.Context, userID, hotelID string) ([]BoardPost, error) {
	if _, _, err := s.activeCheckin(ctx, userID, hotelID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT b.id::text, p.public_ref::text, b.message, b.created_at
		  FROM hotel_board_posts b
		  JOIN hotel_checkins c ON c.id = b.checkin_id AND c.checked_out_at IS NULL
		  JOIN profiles p ON p.user_id = b.user_id
		  JOIN users u ON u.id = b.user_id
		 WHERE b.hotel_id = $1::uuid AND NOT u.is_banned
		   AND `+users.NotBlockedSQL("b.user_id", "$2::uuid")+`
		 ORDER BY b.created_at DESC LIMIT 50`, hotelID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BoardPost{}
	for rows.Next() {
		var p BoardPost
		if err := rows.Scan(&p.ID, &p.AuthorRef, &p.Message, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PostToBoard adds a post tied to the author's check-in (deleted with it).
func (s *Service) PostToBoard(ctx context.Context, userID, hotelID, message string) (*BoardPost, error) {
	checkinID, _, err := s.activeCheckin(ctx, userID, hotelID)
	if err != nil {
		return nil, err
	}
	msg := middleware.SanitizeText(utils.StripTags(message), 280)
	if msg == "" {
		return nil, users.ValidationError{Msg: "message must not be empty"}
	}
	if err := s.users.VetText(ctx, userID, "post", msg); err != nil {
		return nil, err
	}
	var p BoardPost
	err = s.db.QueryRow(ctx, `
		WITH ins AS (
		  INSERT INTO hotel_board_posts (hotel_id, checkin_id, user_id, message)
		  VALUES ($1::uuid, $2::uuid, $3::uuid, $4) RETURNING id, user_id, message, created_at)
		SELECT ins.id::text, p.public_ref::text, ins.message, ins.created_at
		  FROM ins JOIN profiles p ON p.user_id = ins.user_id`, hotelID, checkinID, userID, msg).
		Scan(&p.ID, &p.AuthorRef, &p.Message, &p.CreatedAt)
	return &p, err
}

// SendDrink sends an anonymous virtual drink to someone checked in at the
// same hotel. One per sender/recipient per stay.
func (s *Service) SendDrink(ctx context.Context, userID, hotelID, toRef string) error {
	checkinID, _, err := s.activeCheckin(ctx, userID, hotelID)
	if err != nil {
		return err
	}
	toID, err := s.users.ResolveVisibleRef(ctx, userID, toRef)
	if errors.Is(err, users.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if toID == userID {
		return users.ValidationError{Msg: "you cannot send a drink to yourself"}
	}
	var present bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM hotel_checkins
		                WHERE user_id=$1::uuid AND hotel_id=$2::uuid AND checked_out_at IS NULL)`,
		toID, hotelID).Scan(&present); err != nil {
		return err
	}
	if !present {
		return ErrNotFound
	}
	tag, err := s.db.Exec(ctx, `
		INSERT INTO hotel_drinks (checkin_id, from_id, to_id) VALUES ($1::uuid,$2::uuid,$3::uuid)
		ON CONFLICT (checkin_id, from_id, to_id) DO NOTHING`, checkinID, userID, toID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadySent
	}
	return nil
}

// Drinks lists drinks the caller received on this stay, sender anonymous.
func (s *Service) Drinks(ctx context.Context, userID, hotelID string) ([]Drink, error) {
	if _, _, err := s.activeCheckin(ctx, userID, hotelID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, d.created_at
		  FROM hotel_drinks d
		  JOIN hotel_checkins fc ON fc.id = d.checkin_id AND fc.hotel_id = $2::uuid AND fc.checked_out_at IS NULL
		  JOIN profiles p ON p.user_id = d.from_id
		 WHERE d.to_id = $1::uuid AND `+users.NotBlockedSQL("d.from_id", "$1::uuid")+`
		 ORDER BY d.created_at DESC LIMIT 50`, userID, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Drink{}
	for rows.Next() {
		var d Drink
		if err := rows.Scan(&d.FromRef, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
