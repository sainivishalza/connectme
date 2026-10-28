package users

import "context"

// QuizMinShared is how many questions two people must both have answered
// before a quiz match is shown. Below it the percentage would reveal too much
// about a single answer.
const QuizMinShared = 8

// attachQuiz adds the viewer's quiz match to every card, anonymous ones
// included (that is the point of quiz-first matching). The value is rounded to
// the nearest ten so it cannot be used to read someone's answers one by one.
// A failure must never break a profile list.
func (s *Service) attachQuiz(ctx context.Context, viewerID string, ps []PublicProfile, idx map[string][]int) {
	if viewerID == "" || len(idx) == 0 {
		return
	}
	refs := make([]string, 0, len(idx))
	for ref := range idx {
		refs = append(refs, ref)
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text,
		       (ROUND(100.0 * SUM((a.choice = b.choice)::int) / COUNT(*) / 10) * 10)::int
		  FROM profiles p
		  JOIN compat_answers b ON b.user_id = p.user_id
		  JOIN compat_answers a ON a.qid = b.qid AND a.user_id = $2::uuid
		 WHERE p.public_ref = ANY($1::uuid[]) AND p.user_id <> $2::uuid
		 GROUP BY p.public_ref
		HAVING COUNT(*) >= $3`, refs, viewerID, QuizMinShared)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var pct int
		if rows.Scan(&ref, &pct) != nil {
			continue
		}
		for _, i := range idx[ref] {
			ps[i].QuizMatch = pct
		}
	}
}
