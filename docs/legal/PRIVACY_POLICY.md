# ConnectMe Privacy Policy (DRAFT)

> **Draft for legal review. Not legal advice and not yet in force.** It describes what the software does today. Bracketed items `[...]` must be filled in by the operator, and a lawyer must review it for each market (GDPR, UK GDPR, CCPA/CPRA, PIPL, age-assurance and app-store rules) before launch.

Last updated: [date] · Operator: [legal entity, address] · Contact: [privacy email] · EU/UK representative: [if required]

## 1. Who can use ConnectMe
ConnectMe is for adults aged **18 or older**. We ask for your date of birth at sign-up and refuse anyone under 18. This is a self-declared check, not identity verification. If we learn that someone is under 18 we remove the account.

## 2. What we collect
| Data | Why | Notes |
|---|---|---|
| Email, password (stored only as a salted hash), date of birth | Create and secure your account, enforce 18+ | Your email and birth date are never shown to other members |
| Profile: display name, bio, tags, mood, photos, mode (Open / Incognito / Hotel) | Show your profile according to your chosen mode | Incognito and Hotel profiles show no name, photo or exact age before a mutual reveal |
| Location | Find people nearby and hotel check-in | Exact position stays on our servers and is **never sent to other members**; they see only a rounded distance band |
| Swipes, matches, blocks, reports | Matching and safety | |
| Messages and attachments | Deliver chat | **End-to-end encrypted**: our servers store and relay only ciphertext and cannot read it. History is kept on your devices |
| Purchases (coins, subscriptions, gifts) | Provide paid features | Card details go to our payment processor (Stripe); we never see or store card numbers |
| Technical data: IP address, device and request metadata, security logs | Security, abuse and rate limiting | Logs do not contain passwords or tokens |
| Photo ID check (optional): a photo of an ID document, a selfie holding a short code, and your declared date of birth | Confirm you are who you say you are and are 18 or over | Seen only by our review team, every viewing is logged, and **both photos are deleted as soon as the team decides** (or within [7 days] if nobody reviews them). We keep only the result and when it was decided |
| Automated safety signals | Detect scams, abuse and prohibited content in profile text and photos | See section 5 |

We do not sell personal data and do not use it for third-party advertising.

## 3. Hotel Mode and automatic deletion
Hotel Mode is tied to a check-in. Data created for it (hotel board posts, hotel matches and their chats) is deleted automatically after you check out, or at a fixed maximum stay if you never check out: [24 hours after checkout / max stay value to confirm]. Deletion also removes related messages and attachments.

## 4. How long we keep data
- **Account data:** while your account exists.
- **After you delete your account:** profile, photos, matches, messages, location, device data and sessions are erased immediately. We cancel any subscription first. Some limited records may remain where the law requires or for safety: payment records [period], and a one-way hash of your email if your account was banned (to stop ban evasion).
- **Moderation reports and staff audit log:** kept [period] for safety and legal compliance. Staff access to member data is logged.
- **Backups:** [period]; erased data ages out of backups on this schedule.

## 5. Automated screening and human review
We run profile text and photos through an internal screening service that applies rules (for example clear threats, minors, scams, off-platform solicitation). Ambiguous cases go to a human moderator. Screening is a safety aid, **not** a guarantee that content is safe, and a flag alone does not ban an account. You can contact us to contest a decision [appeals process to be defined]. Chats are end-to-end encrypted and are not scanned; moderators see message content only if a participant reports it.

## 6. Who we share data with
- **Other members:** only what your chosen mode shows.
- **Service providers (processors):** hosting [provider], payments (Stripe), email delivery [provider], image-safety screening [provider], push notifications [provider], under data-processing agreements.
- **Authorities:** when legally required or to protect someone's safety, with the minimum necessary.
We do not share your data with the AI screening component beyond the minimum: profile text, tags, age and activity counts; never your name, exact location or chat content.

## 7. International transfers
[Where servers are located; transfer mechanism such as SCCs.]

## 8. Your rights
Depending on where you live you may access, export, correct or delete your data, object to or restrict processing, withdraw consent, and complain to a regulator.
- **Export:** Profile → Download my data.
- **Delete:** Profile → Delete my account (asks for your password).
- Other requests: [privacy email]. We respond within [30 days].

## 9. Security
Passwords are hashed with bcrypt; sessions use short-lived tokens with rotation and reuse detection; traffic is encrypted in transit; chats are end-to-end encrypted; staff actions are audit-logged. No system is perfectly secure. Report vulnerabilities to [security email].

## 10. Children
We do not knowingly collect data from anyone under 18.

## 11. Changes
We will notify you in the app and by email of material changes [notice period].
