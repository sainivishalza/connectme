# Security route map

Generated from the running router by `UPDATE_SECURITY_ROUTES=1 go test ./internal/integration -run TestWriteSecurityRoutes`.
**Auth**: `public` = on the audited allowlist, `user` = signed access token, `staff` = moderator/admin role.
**Role**: lowest role observed to get past the role check. **Rate limit**: the `X-RateLimit-Limit` the route reports to a signed-in caller; `none` is a finding.
**Ownership**: how the code proves the caller may touch that object. `reviewed` = read in the service code, `spot-checked` = looked at, not proven for every method, `UNREVIEWED` = not looked at.

| Method | Path | Auth | Role | Rate limit | Ownership / object check |
|---|---|---|---|---|---|
| POST | `/auth/change-password` | user | user | 5 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/forgot-password` | public | - | 5 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/login` | public | - | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/logout-all` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/logout` | public | - | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/refresh` | public | - | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/register` | public | - | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/reset-password` | public | - | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/auth/verify-email` | public | - | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/calls/` | user | user | 40 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/calls/incoming` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/calls/{id}/accept` | user | user | 120 | reviewed: caller_id or callee_id of the call (accept/decline: callee only) |
| POST | `/calls/{id}/decline` | user | user | 120 | reviewed: caller_id or callee_id of the call (accept/decline: callee only) |
| POST | `/calls/{id}/end` | user | user | 120 | reviewed: caller_id or callee_id of the call (accept/decline: callee only) |
| GET | `/calls/{id}` | user | user | 90 | reviewed: caller_id or callee_id of the call (accept/decline: callee only) |
| GET | `/chapters/me` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/chapters/me` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/chapters/{ref}` | user | user | 120 | spot-checked: visible-ref resolution |
| GET | `/chat/groups/{id}/envelopes` | user | user | 300 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| POST | `/chat/groups/{id}/envelopes` | user | user | 120 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| GET | `/chat/groups/{id}/keys` | user | user | 60 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| GET | `/chat/keys/count` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/chat/keys/{matchID}` | user | user | 60 | reviewed: chat.participant(), then only the OTHER party's key bundle; consumes one one-time prekey per call (see audit: prekey drain) |
| POST | `/chat/keys` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/chat/matches/{id}/access` | user | user | 600 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| POST | `/chat/matches/{id}/media` | user | user | 30 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| DELETE | `/chat/matches/{id}/messages/{mid}` | user | user | 120 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| GET | `/chat/matches/{id}/messages` | user | user | 600 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| POST | `/chat/matches/{id}/messages` | user | user | 30 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| POST | `/chat/matches/{id}/read` | user | user | 600 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| DELETE | `/chat/matches/{id}` | user | user | 20 | reviewed: chat.participant(): party to an unexpired matched match, or member of a matched group pair; neither user banned; else 404 |
| GET | `/chat/media/{id}` | user | user | 600 | reviewed: media document's match id, then chat.participant() |
| POST | `/communities/` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/communities/mine` | user | user | 90 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/communities/nearby` | user | user | 90 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/communities/{id}/join` | user | user | 60 | spot-checked: community_members membership / owner role |
| DELETE | `/communities/{id}/membership` | user | user | 60 | spot-checked: community_members membership / owner role |
| GET | `/communities/{id}/posts` | user | user | 120 | spot-checked: community_members membership / owner role |
| POST | `/communities/{id}/posts` | user | user | 60 | spot-checked: community_members membership / owner role |
| DELETE | `/communities/{id}` | user | user | 30 | spot-checked: community_members membership / owner role |
| GET | `/digest/me` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/digest/me` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/digest/unsubscribe` | public | - | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/follows/{direction}` | user | user | 120 | reviewed: follows.target(): visible-ref resolution (blocked/hidden look like not found) |
| DELETE | `/follows/{ref}` | user | user | 120 | reviewed: follows.target(): visible-ref resolution (blocked/hidden look like not found) |
| POST | `/follows/{ref}` | user | user | 120 | reviewed: follows.target(): visible-ref resolution (blocked/hidden look like not found) |
| GET | `/forums/boards/{slug}/threads` | user | user | 120 | public board content; reports are per caller |
| POST | `/forums/boards/{slug}/threads` | user | user | 30 | public board content; reports are per caller |
| GET | `/forums/boards` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/forums/boards` | staff | admin | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| DELETE | `/forums/replies/{id}` | user | user | 60 | reviewed for DELETE: author_id = caller; GET is public board content |
| GET | `/forums/threads/{id}/replies` | user | user | 120 | public board content; reports are per caller |
| POST | `/forums/threads/{id}/replies` | user | user | 120 | public board content; reports are per caller |
| DELETE | `/forums/threads/{id}` | user | user | 60 | reviewed for DELETE: author_id = caller; GET is public board content |
| GET | `/forums/threads/{id}` | user | user | 120 | reviewed for DELETE: author_id = caller; GET is public board content |
| POST | `/forums/{kind}/{id}/report` | user | user | 30 | public board content; reports are per caller |
| POST | `/games/group/{id}/mlt/start` | user | user | 60 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/group/{id}/mlt/vote` | user | user | 300 | reviewed: games.member(): caller in the match or matched group |
| GET | `/games/group/{id}/mlt` | user | user | 180 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/group/{id}/start` | user | user | 60 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/group/{id}/vote` | user | user | 300 | reviewed: games.member(): caller in the match or matched group |
| GET | `/games/group/{id}` | user | user | 180 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/masked/join` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/games/masked/leave` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/games/masked/rooms/{id}/answers/{aid}/report` | user | user | 30 | spot-checked: masked_members membership |
| POST | `/games/masked/rooms/{id}/finish` | user | user | 600 | spot-checked: masked_members membership |
| GET | `/games/masked/rooms/{id}` | user | user | 240 | spot-checked: masked_members membership |
| GET | `/games/masked/status` | user | user | 240 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/games/party/{id}/done` | user | user | 600 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/party/{id}/pick` | user | user | 600 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/party/{id}/skip` | user | user | 600 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/party/{id}/start` | user | user | 60 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/party/{id}/take-over` | user | user | 120 | reviewed: games.member(): caller in the match or matched group |
| DELETE | `/games/party/{id}` | user | user | 60 | reviewed: games.member(): caller in the match or matched group |
| GET | `/games/party/{id}` | user | user | 180 | reviewed: games.member(): caller in the match or matched group |
| GET | `/games/prompt` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/games/quiz/match/{id}` | user | user | 90 | reviewed: games.member(): caller in the match or matched group |
| GET | `/games/quiz/sets` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/games/quiz/{id}/answer` | user | user | 400 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/quiz` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/games/td/{id}/done` | user | user | 600 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/td/{id}/pick` | user | user | 600 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/td/{id}/skip` | user | user | 600 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/td/{id}/start` | user | user | 60 | reviewed: games.member(): caller in the match or matched group |
| POST | `/games/td/{id}/take-over` | user | user | 120 | reviewed: games.member(): caller in the match or matched group |
| DELETE | `/games/td/{id}` | user | user | 60 | reviewed: games.member(): caller in the match or matched group |
| GET | `/games/td/{id}` | user | user | 180 | reviewed: games.member(): caller in the match or matched group |
| PUT | `/geo/checkin/mood` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/geo/checkin` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/geo/checkin` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/geo/checkout` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/geo/detect` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/geo/hotels/{id}/board` | user | user | 120 | reviewed: caller has an open check-in at that hotel (activeCheckin) |
| POST | `/geo/hotels/{id}/board` | user | user | 10 | reviewed: caller has an open check-in at that hotel (activeCheckin) |
| POST | `/geo/hotels/{id}/drink` | user | user | 10 | reviewed: caller has an open check-in at that hotel (activeCheckin) |
| GET | `/geo/hotels/{id}/drinks` | user | user | 120 | reviewed: caller has an open check-in at that hotel (activeCheckin) |
| GET | `/geo/hotels/{id}/moods` | user | user | 120 | reviewed: caller has an open check-in at that hotel (activeCheckin) |
| GET | `/geo/hotels/{id}/users` | user | user | 120 | reviewed: caller has an open check-in at that hotel (activeCheckin) |
| POST | `/geo/hotels` | staff | admin | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/gifs/search` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/gifs/{id}` | user | user | 300 | public generated assets |
| GET | `/groups/` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/groups/` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/groups/browse` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/groups/dates/{id}/confirm` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| GET | `/groups/dates/{id}/logistics` | user | user | 300 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/dates/{id}/logistics` | user | user | 30 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| DELETE | `/groups/dates/{id}` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/invites/{id}/accept` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/invites/{id}/decline` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| GET | `/groups/invites` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/groups/matches/{gm}/dates` | user | user | 300 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/matches/{gm}/reply-ideas` | user | user | 20 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| GET | `/groups/matches` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/groups/{id}/invite` | user | user | 30 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/{id}/join` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/{id}/match` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| DELETE | `/groups/{id}/members/me` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/{id}/schedule` | user | user | 60 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| POST | `/groups/{id}/venue` | user | user | 30 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| GET | `/groups/{id}` | user | user | 300 | reviewed for GET /groups/{id}: isMember(); other group routes spot-checked |
| GET | `/healthz` | public | - | none | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/idcheck/queue` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/idcheck/start` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/idcheck/status` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/idcheck/submit` | user | user | 6 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/idcheck/{id}/decision` | staff | moderator | 600 | member: own check; staff: role-gated queue |
| GET | `/idcheck/{id}/image/{which}` | staff | moderator | 600 | member: own check; staff: role-gated queue |
| POST | `/investors/admin/pledges/{id}/{decision}` | staff | admin | 120 | staff permission enforced in middleware (admin role / manage-hotels); not an object owner check |
| GET | `/investors/admin/pledges` | staff | admin | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/investors/mine` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| DELETE | `/investors/pledges/{id}` | user | user | 20 | reviewed: user_id = caller |
| POST | `/investors/pledges` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/investors/public` | public | - | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/levels/board/{kind}` | user | user | 60 | aggregate public boards; opt-in per member; blocks applied per viewer |
| PUT | `/levels/me/visibility` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/levels/me` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/matching/boost` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/matching/likes-you` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/matching/matches/{id}/rescore` | user | user | 10 | reviewed: caller is user1/user2 of the match, status matched |
| DELETE | `/matching/matches/{id}/reveal-timer` | user | user | 30 | reviewed: caller is user1/user2 of the match, status matched |
| POST | `/matching/matches/{id}/reveal-timer` | user | user | 30 | reviewed: caller is user1/user2 of the match, status matched |
| POST | `/matching/matches/{id}/reveal-voice` | user | user | 60 | reviewed: caller is user1/user2 of the match, status matched |
| POST | `/matching/matches/{id}/reveal` | user | user | 60 | reviewed: caller is user1/user2 of the match, status matched |
| GET | `/matching/matches/{id}/voice` | user | user | 120 | reviewed: caller is user1/user2 of the match, status matched |
| DELETE | `/matching/matches/{id}` | user | user | 60 | reviewed: caller is user1/user2 of the match, status matched |
| GET | `/matching/matches` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/matching/rewind` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/matching/superlike` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/matching/swipe` | user | user | 100 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/matching/top-picks` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/media/photos/{id}` | user | user | 600 | reviewed: owner verified, not banned, not blocked, mode visibility must be full; else 404 |
| GET | `/media/voice/{ref}` | user | user | 600 | spot-checked: users.OpenVoiceIntro visibility |
| GET | `/moderation/analytics` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/appeals/{id}/decide` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/appeals` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/appeals` | public | - | 5 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/moderation/audit` | staff | admin | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| DELETE | `/moderation/blocks/{ref}` | user | user | 60 | caller's own block list |
| POST | `/moderation/blocks` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/forum/{kind}/{id}/remove` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/members` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/posts/{id}/remove` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/queue` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/moderation/reports/mine` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/reports/{id}/resolve` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/reports` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/reports` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/moderation/rooms` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/moderation/stats` | staff | moderator | 600 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/moderation/users/{ref}/ban` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/users/{ref}/photos/{id}` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| POST | `/moderation/users/{ref}/revoke-sessions` | staff | admin | 60 | staff role enforced in middleware; per-object staff actions are audited |
| POST | `/moderation/users/{ref}/unban` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/users/{ref}` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| POST | `/moderation/videos/{id}/approve` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| POST | `/moderation/videos/{id}/remove` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/moderation/videos/{id}/ticket` | staff | moderator | 600 | staff role enforced in middleware; per-object staff actions are audited |
| GET | `/payments/balance` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/payments/cancel` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/payments/coins` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/payments/gift` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/payments/gifts/catalog` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/payments/gifts` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/payments/subscribe` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/payments/webhook` | public | - | none | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/privacy/erase` | user | user | 3 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/privacy/export` | user | user | 5 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/push/devices/remove` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/push/devices` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/push/prefs` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/push/prefs` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/quiz/` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/quiz/answers` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/random/cancel` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/random/join` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/random/status` | user | user | 90 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/random/{id}/keep` | user | user | 60 | reviewed: caller is party to a matched random-origin match |
| GET | `/readyz` | public | - | none | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/safety/confirm` | public | - | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/safety/contacts/{id}/phone/confirm` | user | user | 30 | reviewed: SQL filters id AND user_id = caller |
| DELETE | `/safety/contacts/{id}/phone` | user | user | 30 | reviewed: SQL filters id AND user_id = caller |
| PUT | `/safety/contacts/{id}/phone` | user | user | 10 | reviewed: SQL filters id AND user_id = caller |
| DELETE | `/safety/contacts/{id}` | user | user | 30 | reviewed: SQL filters id AND user_id = caller |
| GET | `/safety/contacts` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/safety/contacts` | user | user | 6 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/safety/plans/{id}/extend` | user | user | 60 | reviewed: SQL filters id AND user_id = caller |
| POST | `/safety/plans/{id}/{how}` | user | user | 60 | reviewed: SQL filters id AND user_id = caller |
| GET | `/safety/plans` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/safety/plans` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/scamcheck/{conv}` | user | user | 30 | reviewed: caller is party to the match or a matched group |
| GET | `/signal/{kind}/{id}/access` | user | user | 600 | reviewed: signal.Allowed(): per-kind membership |
| POST | `/social/claims/{id}/accept` | user | user | 120 | reviewed: owned(): caller is the post's author |
| POST | `/social/claims/{id}/decline` | user | user | 120 | reviewed: owned(): caller is the post's author |
| POST | `/social/comments/{id}/report` | user | user | 30 | spot-checked: author of the comment or of the post |
| DELETE | `/social/comments/{id}` | user | user | 120 | spot-checked: author of the comment or of the post |
| GET | `/social/feed` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/social/posts/mine` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/social/posts/{id}/claim` | user | user | 20 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| GET | `/social/posts/{id}/claims` | user | user | 60 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| GET | `/social/posts/{id}/comments` | user | user | 120 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| POST | `/social/posts/{id}/comments` | user | user | 60 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| POST | `/social/posts/{id}/like` | user | user | 300 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| POST | `/social/posts/{id}/report` | user | user | 30 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| DELETE | `/social/posts/{id}` | user | user | 120 | reviewed: social.canSee(): not blocked/banned and within radius, same hotel stay, or community member |
| POST | `/social/posts` | user | user | 40 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/sounds/` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/sounds/{id}` | user | user | 600 | public generated assets |
| GET | `/sponsors/` | staff | admin | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/sponsors/` | staff | admin | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/sponsors/feed` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/sponsors/{id}/click` | user | user | 120 | staff permission enforced in middleware (admin role / manage-hotels); not an object owner check |
| POST | `/sponsors/{id}/pause` | staff | admin | 120 | staff permission enforced in middleware (admin role / manage-hotels); not an object owner check |
| POST | `/sponsors/{id}/resume` | staff | admin | 120 | staff permission enforced in middleware (admin role / manage-hotels); not an object owner check |
| PUT | `/users/me/details` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/users/me/invisible` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/users/me/location` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/users/me/mode` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/users/me/onboarded` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/users/me/photos/order` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/users/me/photos/{id}/caption` | user | user | 60 | reviewed: photo id must be in the caller's own profile photos |
| PUT | `/users/me/photos/{id}/main` | user | user | 60 | reviewed: photo id must be in the caller's own profile photos |
| DELETE | `/users/me/photos/{id}` | user | user | 20 | reviewed: photo id must be in the caller's own profile photos |
| POST | `/users/me/photos` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/users/me/prompts` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| DELETE | `/users/me/voice-intro` | user | user | 20 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PUT | `/users/me/voice-intro` | user | user | 10 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/users/me` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| PATCH | `/users/me` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/users/nearby` | user | user | 60 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/users/prompts` | user | user | 120 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/users/{ref}` | user | user | 300 | spot-checked: users.GetPublic(): ResolveVisibleRef + projection by relation and mode |
| POST | `/videos/` | user | user | 30 | n/a: acts on the caller's own data (user id comes from the signed token) |
| POST | `/videos/comments/{cid}/report` | user | user | 30 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| DELETE | `/videos/comments/{cid}` | user | user | 120 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| GET | `/videos/feed` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/videos/file/{id}` | public | - | 1200 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| GET | `/videos/mine` | user | user | 300 | n/a: acts on the caller's own data (user id comes from the signed token) |
| GET | `/videos/reel/{ref}` | user | user | 300 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| GET | `/videos/{id}/comments` | user | user | 120 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| POST | `/videos/{id}/comments` | user | user | 60 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| POST | `/videos/{id}/like` | user | user | 240 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| POST | `/videos/{id}/report` | user | user | 20 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| GET | `/videos/{id}/ticket` | user | user | 600 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |
| DELETE | `/videos/{id}` | user | user | 60 | spot-checked: open-mode, verified, not banned, not blocked; delete: owner; file: short-lived HMAC ticket |

## Gaps found by the generator

- Routes reporting no rate limit: 0
- Routes with an identifier and no reviewed ownership rule: 0

## WebSocket events (Node relay, realtime/src/server.js)

Authentication is the signed access token sent in the Socket.IO connect packet (not in the URL). Every event shares one per-user budget of 240/min; messages have their own 30/min. Both budgets live in the relay's memory (see the audit, finding on rate limits).

| Event | Auth | Object check | Limit |
|---|---|---|---|
| connect | access token verified (HS256, issuer, typ=access) | - | none per IP, no cap on sockets per user |
| authenticate | same subject as the connection | - | 240/min/user |
| join_match | token | API `GET /chat/matches/{id}/access` (chat.participant) | 240/min/user |
| leave_match | token | - | 240/min/user |
| watch / unwatch | token | API `GET /signal/{kind}/{id}/access`; max 8 per socket | 240/min/user |
| message | token, room joined | API `POST /chat/matches/{id}/messages` re-checks participant | 30/min/user |
| typing | token, room joined | re-check via access endpoint every 20 s | 240/min/user |
| read_receipt | token, room joined | API `POST /chat/matches/{id}/read` | 240/min/user |

Server pushes: `message`, `typing`, `read_receipt`, `match_closed`, `auth_expired`, `signal`.
