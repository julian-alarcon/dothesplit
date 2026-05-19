# Features

Detailed reference for what currently ships in DoTheSplit. The
[README](../README.md) keeps a one-liner per area; this file is the long form.

## Accounts

Register, log in, log out, change display name, change password (old password
required). Set a personal **timezone** override (otherwise resolved from a
device-detected `dts_tz` cookie, falls back to UTC). Upload an **8×8 pixel
avatar** generated in-browser from any image: pixelated PNG ≤ 1024 bytes,
re-encoded server-side; falls back to deterministic initials when absent.
Soft-delete your account with a stable `Deleted user #<short-uuid>` tombstone so
shared history stays traceable; the email index is partial-unique on
`deleted_at IS NULL`, so the address is reusable after deletion.

## First-run setup

The first boot prints a single-use **setup token** in the API container log
(`docker compose logs api`). Until that token is consumed via `/setup`, every
UI route redirects there and `/v1/auth/register` returns
`403 setup_required`. The token is 32 bytes of `crypto/rand` entropy, stored
only as SHA-256, rotated on every boot, and consumed atomically with the first
admin's account creation in a single transaction (advisory lock + `SELECT FOR
UPDATE`). Replays return `410 Gone`. After completion the page is closed:
authenticated visitors are redirected to `/groups`, the rest to `/login`.

## Admin role

A separate `is_admin` flag on `users`, granted to the bootstrap admin and any
user another admin promotes. The `/admin` area exposes:

- **Users**: list, search, toggle admin, reset password (forces a change on
  next login via `must_change_password`), soft-delete, optional
  `?include_deleted=1` toggle.
- **Groups**: oversight view of every group with member count and creator.
- **SMTP**: configure outbound mail; the password column is encrypted at rest
  with AES-GCM using `EMAIL_ENC_KEY`.
- **Audit**: paginated log of every admin action with actor, IP, UA, target.

Destructive actions (delete user, reset password, toggle admin) require a
**step-up password prompt** in the same browser session, even if the admin is
already logged in. SMTP is exempt (configuration, not a destructive action).

## Groups

Create, rename, set a per-group **default currency** (defaults to EUR; full
list: EUR, USD, GBP, CHF, CAD, AUD, JPY, SEK, NOK, DKK, plus a few others).
Invite existing members by email, leave a group, remove a member (creator
only), **transfer ownership** to another member, delete (creator only;
cascades to expenses, splits, settlements, recurring templates). Settings live
on a dedicated `/groups/{id}/settings` page. For 2-member groups, pin a
**default percentage split** (e.g. 60/40) that prefills new expenses;
auto-cleared when a 3rd member joins. Group creation supports adding initial
members in the same form.

## Expenses

Three split modes via a shared in-app editor:

- **equal**: round-robin remainder distribution
- **exact**: per-member cents with live remainder validation
- **percent**: per-member percentage with live total validation

Expenses carry a category (one of ten seeded categories, rendered with Font
Awesome icons), an optional custom date (defaults to today), and a
description. Any group member can edit description / amount / category / payer
/ splits / date after the fact; splits either rescale proportionally on
amount-only edits or are re-resolved when a new mode/split is supplied.
Soft-delete is open to any group member. The full edit history shows who /
when / field / old → new, including per-member split diffs.

## Balances & settle-up

Net-balance computation over all expenses + settlements, plus a simplified "X
owes Y" view. Settlements are recorded directly from the group page, appear in
the same paginated activity feed as expenses, and have their own detail page.

## Recurring expenses

Templates with daily / weekly / biweekly / monthly / yearly cadence. A
separate Go worker materializes a real expense on each cadence tick. Both the
API and the UI (`/groups/{id}/recurring`) are shipped.

## Activity feed

Paginated, time-ordered feed of expenses + settlements per group. Months are
labelled, ordering matches the underlying timestamps regardless of insertion
order, and pagination state is URL-encoded so deep links work.

## Security

- Argon2id passwords with a server-side pepper.
- Email stored as HMAC (lookup) + AES-GCM (display) with 32-byte keys held in
  env, never in the DB.
- Rate-limited `/v1/auth/*` and `/v1/setup/admin` (5/min/IP).
- Strict JSON bodies: unknown fields are a 400.
- CSP headers with SHA-256 hashes on inline scripts; no inline event handlers
  (e.g. `onchange`): auto-submit forms use a `data-auto-submit` attribute and
  a shared module.
- HSTS only when `COOKIE_SECURE=true`. Session cookie is `__Host-dts_session`
  on HTTPS, plain `dts_session` on the HTTP LAN profile.
- Step-up password prompt for destructive admin actions.

## API contract

OpenAPI 3.0.3 at [openapi.yaml](openapi.yaml) is the source of truth. Every
business endpoint lives under `/v1/...`; health probes (`/healthz`, `/readyz`)
are the only unversioned routes. Go server types and a Go integration-test
client are generated via `oapi-codegen`; TypeScript types via
`openapi-typescript`. `make gen` regenerates both.
