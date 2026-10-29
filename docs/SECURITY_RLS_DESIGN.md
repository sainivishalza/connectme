# Database least privilege and row-level security (for your approval, nothing is built)

**Why.** Today the app connects to Postgres as `connectme`, which the Postgres image makes a **superuser** and the **owner of every table**. A single forgotten `WHERE user_id = ...`, or one SQL bug, can read or change every member's rows, and the database will not stop it. Authorization lives only in Go code. This adds a second lock.

**Recommendation from your delegation:** do **Step 1 before launch** and **Step 2 after launch**.

## Step 1: a least-privilege database account (before launch)

| Role | Used by | Rights |
|---|---|---|
| `connectme_owner` | schema migrations only (a separate DSN, `POSTGRES_MIGRATE_URL`) | owns the tables, runs migrations; not used at runtime |
| `connectme_app` | the API and its jobs at runtime (`POSTGRES_URL`) | login; `SELECT, INSERT, UPDATE, DELETE` on tables, `USAGE` on sequences; **no** superuser, createdb, createrole, `TRUNCATE`, DDL, or table ownership |
| `connectme_readonly` | support and analytics | `SELECT` on a reviewed list of tables |

- Roles are created by an infrastructure bootstrap script (`ops/db/bootstrap.sql`), not by migrations, because migrations must not need the right to create roles. The compose init script runs it for development.
- Migrations create tables as `connectme_owner`; a default-privileges grant gives `connectme_app` its rights on every future table.
- **Startup check:** production refuses to start if the runtime role is a superuser, has `BYPASSRLS`, or owns any table. The check is one query against `pg_roles` and `pg_tables`, covered by a test.
- Effort: small (a script, a second DSN, one config check, tests). No application query changes.

## Step 2: row-level security (after launch, if you approve)

**What it is not.** It does not replace the ownership checks in Go. It makes a missing check return nothing instead of everyone's data.

### Which tables

| Class | Examples | Treatment |
|---|---|---|
| A. Strictly private to one member | `push_devices`, `push_prefs`, `trusted_contacts`, `date_plans`, `email_tokens`, `coin_ledger`, `payments`, `subscriptions`, `one_time_prekeys`, `id_checks` (member side), `prekey_fetches`, `csam_hits` (staff only) | RLS with `user_id = current_setting('app.user_id')::uuid` |
| B. Shared by exactly two people or a small group | `matches`, `calls`, `reveal_consents`, `date_plans` logistics, group tables | RLS with membership: `current_setting('app.user_id')::uuid IN (user1_id, user2_id)` |
| C. Built to be seen by many (discovery) | `profiles`, `posts`, `videos`, `forum_*`, `communities` | **No RLS.** Visibility rules (blocked, hidden, incognito, radius) are too rich for a policy and policies would have to join other tables on every row. These stay protected by the Go checks and the shared helper. |

### How the current member reaches the database

- A small transaction wrapper, `db.AsUser(ctx, userID, func(tx pgx.Tx) error)`, runs `SELECT set_config('app.user_id', $1, true)` as the first statement. The setting lasts for that transaction only (`SET LOCAL` semantics), so a pooled connection can never carry one member's identity into the next request.
- Outside such a transaction the setting is empty, the policies match nothing, and the table looks empty. A forgotten wrapper fails safe.
- Policies are written as `(SELECT current_setting('app.user_id', true)::uuid)` so Postgres evaluates the setting once per query, not once per row.
- Cost: one extra statement per transaction (it can ride in the same round trip as the first query). Expect well under a millisecond.
- Compatible with PgBouncer in transaction mode.

### Background jobs, webhooks, staff

Work with no signed-in member (retention jobs, the Stripe webhook, the push sender, moderation queues) uses a second pool as `connectme_system`, a separate login role with `BYPASSRLS` that can only be used from those code paths. Its use is explicit and listed in `docs/SECURITY_ROUTES.md`. Staff actions on a member go through the system pool **and** are audited.

### Rollout (one table per release, always reversible)

1. Bring up Step 1 in staging and run the full test suite as `connectme_app`.
2. Per table: enable RLS with a permissive `USING (true)` policy, run the suite and watch for errors, then replace it with the real policy.
3. Never `FORCE` and enable in the same release as a code change.
4. **Rollback** is a new forward migration, `ALTER TABLE ... DISABLE ROW LEVEL SECURITY`. No data is dropped or rewritten at any step.

### The test that proves it

For every table in classes A and B, a "forgot the WHERE clause" test connects as `connectme_app`, sets the identity to member A, and runs a bare `SELECT count(*) FROM <table>`: it must return only A's rows. With no identity set it must return 0. An `INSERT` or `UPDATE` that names member B must fail (`WITH CHECK`). It is generated from a list of tables, so a new private table cannot be added without its policy.

## Risks

- A wrongly written policy can hide rows from their owner. Mitigated by the one-table-at-a-time rollout and the generated test.
- Some queries that join across members (matching, nearby) touch class C tables only; none touch A or B, so they are unaffected.
- Operational: two DSNs and three roles must be kept straight. The startup check catches the dangerous mistakes.

## Needs your approval

1. Step 1 before launch: yes or no?
2. Step 2 after launch: yes, no, or only for the most sensitive tables first (`coin_ledger`, `payments`, `trusted_contacts`, `date_plans`, key tables)?
3. Who runs the bootstrap script in production (it needs database admin rights)?

Nothing is implemented until you answer.
