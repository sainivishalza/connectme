# Moderator dashboard

Embedded single-page app served at `/admin/` by the API. Staff sign in with a normal moderator/admin account; the page calls the `/moderation/*` API, so all authorization and audit logging happen server-side.

- Files: `web/index.html`, `web/app.js`, `web/style.css` (only these three are served).
- CSP forbids inline script/style and third-party origins; user text is rendered with `textContent` only. Keep it that way: no `innerHTML`.
- Views: report queue + stats, user lookup (ban/unban with reason), audit log (admin only).
- Tokens: access token in memory, refresh token in `sessionStorage` (cleared when the tab closes).
- Dev: seed a staff account by setting `users.role` to `moderator`/`admin` in the database.
