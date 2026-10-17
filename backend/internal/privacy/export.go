package privacy

import (
	"context"
	"encoding/json"
	"time"
)

// Export is everything the server holds about a person, in a portable form.
// It deliberately contains no other person's data: counterparts appear only
// as the opaque refs the user already knows. Chat content is not here
// because it is end-to-end encrypted: it exists only on the user's devices.
type Export struct {
	GeneratedAt   time.Time        `json:"generated_at"`
	Account       map[string]any   `json:"account"`
	Profile       map[string]any   `json:"profile"`
	Matches       []map[string]any `json:"matches"`
	Swipes        map[string]int   `json:"swipes"`
	Blocks        int              `json:"blocks"`
	Groups        []map[string]any `json:"groups"`
	Payments      []map[string]any `json:"payments"`
	Subscriptions []map[string]any `json:"subscriptions"`
	Coins         map[string]any   `json:"coins"`
	Gifts         map[string]int   `json:"gifts"`
	Reports       map[string]int   `json:"reports"`
	Notes         []string         `json:"notes"`
}

func (s *Service) Export(ctx context.Context, userID string) (*Export, error) {
	ex := &Export{
		GeneratedAt: s.now().UTC(),
		Swipes:      map[string]int{},
		Notes: []string{
			"Chat messages are end-to-end encrypted and are stored only on your devices; the server cannot read or export them.",
			"Exact coordinates are held server-side to compute coarse distances and are included below only as a yes/no.",
		},
	}

	var email, role string
	var phone *string
	var dob, created time.Time
	var verified, banned bool
	var idVerified *time.Time
	err := s.db.QueryRow(ctx, `
		SELECT email, phone, date_of_birth, role, is_verified, is_banned, created_at, id_verified_at
		  FROM users WHERE id=$1::uuid`, userID).
		Scan(&email, &phone, &dob, &role, &verified, &banned, &created, &idVerified)
	if err != nil {
		return nil, ErrNotFound
	}
	ex.Account = map[string]any{
		"email": email, "phone": phone, "date_of_birth": dob.Format("2006-01-02"), "role": role,
		"email_verified": verified, "created_at": created, "id_verified": idVerified != nil,
	}

	var name, bio, intro, gender, looking, mood, mode *string
	var tags []string
	var photos []byte
	var hasLoc, invisible bool
	err = s.db.QueryRow(ctx, `
		SELECT display_name, bio, intro, gender, looking_for, mood, mode, tags, photos,
		       location IS NOT NULL, invisible
		  FROM profiles WHERE user_id=$1::uuid`, userID).
		Scan(&name, &bio, &intro, &gender, &looking, &mood, &mode, &tags, &photos, &hasLoc, &invisible)
	if err == nil {
		var ph []map[string]any
		_ = json.Unmarshal(photos, &ph)
		ex.Profile = map[string]any{
			"display_name": name, "bio": bio, "intro": intro, "gender": gender, "looking_for": looking,
			"mood": mood, "mode": mode, "tags": tags, "photo_count": len(ph), "has_location": hasLoc,
			"invisible": invisible,
		}
	}

	ex.Matches, err = s.maps(ctx, `
		SELECT m.id::text AS match_id, m.mode, m.status, m.created_at, m.compat_score,
		       (SELECT public_ref::text FROM profiles p
		         WHERE p.user_id = CASE WHEN m.user1_id=$1::uuid THEN m.user2_id ELSE m.user1_id END) AS counterpart_ref
		  FROM matches m WHERE m.user1_id=$1::uuid OR m.user2_id=$1::uuid ORDER BY m.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(ctx, `SELECT action, COUNT(*) FROM swipes WHERE actor_id=$1::uuid GROUP BY action`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a string
		var n int
		if err := rows.Scan(&a, &n); err != nil {
			rows.Close()
			return nil, err
		}
		ex.Swipes[a] = n
	}
	rows.Close()

	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM blocks WHERE blocker_id=$1::uuid`, userID).Scan(&ex.Blocks); err != nil {
		return nil, err
	}
	if ex.Groups, err = s.maps(ctx, `
		SELECT g.id::text AS group_id, g.name, g.type, g.activity, m.role, m.joined_at
		  FROM group_members m JOIN groups g ON g.id = m.group_id WHERE m.user_id=$1::uuid`, userID); err != nil {
		return nil, err
	}
	if ex.Payments, err = s.maps(ctx, `
		SELECT type, amount_cents, currency, status, coins, created_at
		  FROM payments WHERE user_id=$1::uuid ORDER BY created_at DESC`, userID); err != nil {
		return nil, err
	}
	if ex.Subscriptions, err = s.maps(ctx, `
		SELECT tier, status, started_at, expires_at, cancel_at_period_end
		  FROM subscriptions WHERE user_id=$1::uuid ORDER BY started_at DESC`, userID); err != nil {
		return nil, err
	}

	var balance int64
	var entries int
	if err := s.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta),0)::bigint, COUNT(*) FROM coin_ledger WHERE user_id=$1::uuid`, userID).
		Scan(&balance, &entries); err != nil {
		return nil, err
	}
	ex.Coins = map[string]any{"balance": balance, "ledger_entries": entries}

	var sent, received, filed, against int
	if err := s.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM gifts_sent WHERE from_user=$1::uuid),
		       (SELECT COUNT(*) FROM gifts_sent WHERE to_user=$1::uuid),
		       (SELECT COUNT(*) FROM reports WHERE reporter_id=$1::uuid),
		       (SELECT COUNT(*) FROM reports WHERE reported_id=$1::uuid)`, userID).
		Scan(&sent, &received, &filed, &against); err != nil {
		return nil, err
	}
	ex.Gifts = map[string]int{"sent": sent, "received": received}
	ex.Reports = map[string]int{"filed_by_you": filed, "about_you": against}
	return ex, nil
}

// maps runs a query and returns each row as a column-name keyed map.
func (s *Service) maps(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := rows.FieldDescriptions()
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c.Name] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
