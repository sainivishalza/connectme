# ConnectMe: product-depth roadmap

The first build covered every feature in the PRD at a basic level. This is the plan for making each area feel like the apps it is modelled on. Work is local only until the owner says otherwise; commit, push and payments wait.

Status: A and B done (rich profiles; photo-first swipe deck with photo carousel, match screen, rewind, like with a note, top picks). C (chat): replies, reactions, unsend, day separators, message search, GIFs and the safety toolkit (trusted contacts, date plans with a check-in, on-device scam warnings) done. F (onboarding, retention, accessibility, language): weekly summary email and the translation scaffolding (en, hi, es for navigation and Appearance) done; the rest of the text is not yet translated.  contrast and large-text tests, screen-reader deck, appearance settings (dark mode choice, text size, reduce motion) done;  guided welcome and quiet hours done; weekly digest, localisation scaffolding and an accessibility pass remain. E (anonymous depth): quiz-first matching, voice-first reveal step and voice-room topics done; timed reveals and quiz-based discovery ordering remain. D (Momo social layer): levels, leaderboards, follows with live alerts, live topics and pinned message, and post comments done; co-hosts, viewer gift animations, comment pushes and short-video comments/follow filter are done too; event groups and room themes done; sounds on short videos deliberately left out (needs music licensing).

## A. Rich profiles and smarter discovery (done)
Prompts, lifestyle and goal answers, details, ID badge, completeness meter, filters on what people want, a match score that weighs it.

## B. Discovery that feels like Tinder / Hinge
- Photo carousel on cards (swipe through photos, dots, tap zones), full-screen profile view
- Rewind the last pass; daily top picks; "like with a comment" on a photo or prompt
- Match celebration screen; "It's a match" with both photos and a first-message suggestion
- Seen-you / likes-you with blur for non-subscribers; boost visibility timer

## C. Chat that feels modern
- Reactions, replies to a message, unsend, typing and read state, message search
- Photo and GIF sharing, voice notes with waveform, link previews off by default (privacy)
- Conversation starters from the other person's prompts; icebreaker prompts in new matches
- Safety toolkit in chat: report, block, share a date plan with a trusted contact

## D. Momo-style social layer
- Levels and badges from activity and gifts; leaderboards for live hosts and gifters
- Richer live rooms: co-hosts, pinned messages, gift animations, room themes, follow hosts
- Nearby feed with reactions and comments, topic channels, event groups
- Short-video feed improvements: follows, sounds, comments

## E. HeyMandi-style anonymous depth
- Quiz-first matching (answer questions, see compatibility before photos)
- Mutual reveal steps (voice first, then photos), reveal timers
- Anonymous voice chat rooms with topics

## F. Onboarding, retention, polish
- Guided onboarding wizard (photos, goal, lifestyle, prompts, location, notifications)
- Notification preferences by type, quiet hours, weekly digest
- Empty-state and error-state polish, accessibility, dark mode review, localisation scaffolding

Each item ships with tests and a note in `docs/DECISIONS.md`.

## G. Improvements pass (built)
- Push signals for live rooms, games, masked rooms and event boards; cached leaderboards; 5 s reveal job
- Scam warnings in Hindi and Spanish; quiz change cap; masked room cooldown and evidence; alerts to all confirmed contacts
- Bottom bar labels; photo order, main photo and captions
- Fixed-price investor model; host payout ledger
- Staff trends, report reasons and member search; match-score tuning tool; backup drill

Still needs outside help: real phones, a native-speaker review of Hindi and Spanish, a lawyer for the investor page and payouts, a moderation team, SMTP/push/LiveKit/Stripe accounts, real outcome data for score tuning, and a drill of point-in-time recovery.

