package users

import "context"

// attachFollows adds the follower count and the viewer's own follow state to
// profiles shown in full. Anonymous cards never carry either: nobody can follow
// someone they cannot see. A failure here must never break a profile list.
func (s *Service) attachFollows(ctx context.Context, viewerID string, ps []PublicProfile, idx map[string][]int) {
	var refs []string
	for ref, is := range idx {
		for _, i := range is {
			if !ps[i].Anonymous {
				refs = append(refs, ref)
				break
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.public_ref::text, COUNT(f.follower_id)::int,
		       COALESCE(BOOL_OR(f.follower_id = NULLIF($2, '')::uuid), FALSE)
		  FROM profiles p JOIN follows f ON f.followee_id = p.user_id
		 WHERE p.public_ref = ANY($1::uuid[])
		 GROUP BY p.public_ref`, refs, viewerID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var n int
		var mine bool
		if rows.Scan(&ref, &n, &mine) != nil {
			continue
		}
		for _, i := range idx[ref] {
			if !ps[i].Anonymous {
				ps[i].Followers, ps[i].Following = n, mine
			}
		}
	}
}
