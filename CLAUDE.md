# CLAUDE.md

Project-specific instructions for Claude Code. Loaded automatically for every session in this repo.

See also: [BLUEPRINT.md](BLUEPRINT.md) for product scope and [README.md](README.md) for local setup.

## What this project is

DoTheSplit — a expense-sharing app.

- **Backend**: Go 1.25, Gin, pgx/v5, `golang-migrate`, `oapi-codegen`. Source in [api/](api/).
- **Frontend**: Astro 6 (SSR, `@astrojs/node`) + Tailwind v4. No React islands, no component library — pages are pure `.astro` + tiny ES-module scripts under [web/src/scripts/](web/src/scripts/). Source in [web/](web/).
- **Database**: PostgreSQL 16. Migrations in [api/migrations/](api/migrations/).
- **Worker**: separate Go binary for recurring expenses ([api/cmd/worker/](api/cmd/worker/)).
- **Infra**: Docker Compose on TrueNAS LAN (HTTP-only — see "Cookie naming" below).

## The golden rule: contract-first

**[docs/openapi.yaml](docs/openapi.yaml) is the source of truth.** Go server types, a Go client for integration tests, and TypeScript types for the web client are all generated from it.

**Order for any user-facing change:**

1. Edit [docs/openapi.yaml](docs/openapi.yaml) first — schemas, paths, responses.
2. Run `make gen` to regenerate Go (`api/internal/apigen/`) and TypeScript (`web/src/lib/api/schema.d.ts`) types. The build won't compile until the code matches.
3. Add a migration if the DB schema changes ([api/migrations/NNNN_*.up.sql](api/migrations/) + matching `.down.sql`).
4. Wire the backend in this exact order: **repo → service → handlers → router**.
5. Regenerate / rebuild the frontend against the new types; add/adjust Astro pages and SSR API routes.
6. Update the worker only if the recurring flow is affected — it reuses services, so most changes propagate for free.
7. Build, test, rebuild the affected containers.

Don't change generated files by hand — re-run `make gen` instead.

### OpenAPI conventions we enforce

- **Spec version**: `3.0.3`. We'd use 3.1 but `oapi-codegen` doesn't fully support it yet — keep it on 3.0.3 until that changes.
- **API versioning**: all business endpoints live under `/v1/…`. Health probes (`/healthz`, `/readyz`) are *not* versioned. Breaking changes cut a new `/v2`, run both mounts in parallel during migration, then retire `/v1` when clients are gone.
- **Request bodies**: must have `additionalProperties: false`. Unknown fields are a 400 — typos should fail loudly, not silently.
- **Error responses**: always reference the named `components.responses.{BadRequest,Unauthorized,Forbidden,NotFound,Conflict,TooManyRequests,ServiceUnavailable}` — never inline `schema: { $ref: ".../Error" }` under a status code.
- **Examples**: add an `example:` to request schemas that anyone would want to try in a docs viewer (create/login flows at minimum).
- **Tags**: every operation has a tag; every tag has a description at the top of the spec.
- **`operationId`**: camelCase verb-object (`createGroup`, `listExpenses`); drives the generated function name in both Go and TS clients.

## Backend layering (strict)

```
handlers → services → repositories → DB
```

- **Handlers** ([api/internal/handlers/](api/internal/handlers/)): bind JSON, call services, translate errors to HTTP status codes. No business logic. Use `errors.Is` on service sentinels.
- **Services** ([api/internal/service/](api/internal/service/)): validate, orchestrate, enforce invariants. Return sentinel errors (`ErrNotMember`, `ErrBadSplit`, etc.). Use transactions for anything that writes more than one table.
- **Repositories** ([api/internal/repo/](api/internal/repo/)): pgx SQL, no domain rules. Map `pgx.ErrNoRows` → `repo.ErrNotFound`.
- **Router** ([api/internal/server/router.go](api/internal/server/router.go)): register endpoints; guard non-auth routes with `mw.RequireSession()`.

Rules of thumb:

- Always call `GroupService.RequireMember` (or equivalent `IsMember`) before reading/writing group-scoped data.
- Expense creation must validate: split-sum invariant, payer ∈ members, all split users ∈ members, mode matches supplied values. All inside one tx.
- Currency is optional on the wire. Empty string means "use the group's `default_currency`" — the service layer looks it up.
- Soft-delete via `deleted_at` for expenses; every read filters `WHERE deleted_at IS NULL` or joins with that filter.

## Database

- PostgreSQL **18** in compose. The volume mounts at `/var/lib/postgresql` (the parent, not `/var/lib/postgresql/data` like in PG 16) because PG 18's image stores data in a version-specific subdir (`/var/lib/postgresql/18/docker/`) so `pg_upgrade --link` can work across majors. Mounting at `data` makes the container fail to start — if you ever see "unused mount/volume" in the Postgres logs, that's the cause.
- Major-version upgrades require `pg_upgrade` or `pg_dump`/`pg_restore`. A plain image bump leaves the old data files unreadable.
- Migrations are append-only. Never edit a committed `*.up.sql`; add a new migration.
- Every migration needs a matching `.down.sql`.
- Keep FK cascades explicit. Group deletion cascades to `group_members`, `expenses` (→ `splits`), `settlements`, `recurring_expenses`.
- Amounts are `BIGINT` cents. IDs are UUIDs with `gen_random_uuid()`.
- Apply locally with `make migrate-up` or let the Docker `migrate` one-shot do it on `up`.

## Frontend conventions

- **Pages under [web/src/pages/](web/src/pages/) render on the server** via `@astrojs/node`. Auth state comes from [middleware.ts](web/src/middleware.ts), which reads the session cookie and calls `/me`.
- **SSR API routes (`web/src/pages/api/*.ts`) must use `process.env`**, not `import.meta.env`, for server-only env vars like `API_BASE_URL_INTERNAL`. `import.meta.env` only exposes `PUBLIC_`-prefixed vars — other values compile to `undefined` and silently fall back to defaults.
- **Money formatting**: always `Intl.NumberFormat(undefined, { style: "currency", currency: <iso>, currencyDisplay: "narrowSymbol" })` with the group's `default_currency` (or the expense's own `currency` for per-expense display). Never hardcode `$`.
- **Currency dropdowns**: default to `EUR`; use the short canonical list (`EUR, USD, GBP, CHF, CAD, AUD, JPY, SEK, NOK, DKK`).
- **Form endpoints post to `/api/*.ts` SSR handlers**, which forward to the Go API with the cookie. Don't call the Go API from client islands if a form-post pattern works — keeps the session on the Astro origin.
- **Optimistic UI / react-query** is planned for richer islands; static forms are fine for CRUD pages.
- Astro's `security.checkOrigin` is disabled — we rely on `SameSite=Lax` on the session cookie for CSRF protection (see [astro.config.mjs](web/astro.config.mjs)).

## Cookie naming (important)

The session cookie's name depends on transport:

- **HTTPS** (`COOKIE_SECURE=true`): `__Host-dts_session` — the `__Host-` prefix enforces `Secure` + no `Domain`.
- **Plain HTTP** (LAN deployment, `COOKIE_SECURE=false`): `dts_session`. The `__Host-` prefix is browser-rejected without `Secure`, so we drop it.

On the backend, use `middleware.SessionCookieName(cfg.CookieSecure)` — never hardcode the name. On the frontend ([web/src/middleware.ts](web/src/middleware.ts)), match with the substring `dts_session=`, which covers both variants.

## Account invariants

- **Soft delete, never hard delete.** Accounts have `deleted_at`; the foreign keys from expenses, splits, settlements, and recurring templates deliberately stay pointing at the tombstoned row so ledgers survive. If a requirement ever seems to want hard delete + CASCADE, stop and flag it — that's silent data loss for every other group member.
- **Tombstone format** is `"Deleted user #" + uuid[:8]`. It's stable (members can still identify *which* deleted person paid for what) and non-identifying (no email, no real name). The full UUID is also the only non-scrambled column after delete, so operators can still answer "who was this?" from the audit trail.
- **Re-registration** with a soft-deleted email works because `users_email_hash_active_key` is a partial unique index (`WHERE deleted_at IS NULL`).
- **Session revocation on delete + password change** — both flows must call `SessionRepo.DeleteAllForUser` so the old cookie stops working immediately. Password change additionally issues a fresh session so the current browser stays logged in.

## Avatar invariants

- Avatars are **uploaded as an 8×8 PNG, ≤ 1024 bytes** (64 color samples). Client-side pipeline in [web/src/scripts/avatar-pixelate.ts](web/src/scripts/avatar-pixelate.ts) center-crops any source image to square, downsamples with `imageSmoothingEnabled = false` (nearest-neighbour), and pushes saturation to 1.0 before base64-encoding a PNG. The server **re-encodes from a fresh RGBA canvas and nearest-neighbour upscales to `AvatarRenderSize`** (currently 256×256 = 8 × 32) before storing in `users.avatar BYTEA`. The pre-scaled bitmap renders crisp at any CSS size without `image-rendering: pixelated` hints, which have inconsistent browser support.
- GDPR-safe by construction: 64 pixels can't identify a human. Never add a "keep original" option without legal sign-off.
- Fallback when `has_avatar=false` is handled by [web/src/components/Avatar.astro](web/src/components/Avatar.astro) — initials from the display name + a deterministic HSL color seeded on the UUID. Don't store initials or the color anywhere; they're pure derivations.

## Security invariants (don't regress)

- Passwords: Argon2id only, `golang.org/x/crypto/argon2`. Never accept reversibly-encrypted passwords.
- Emails: `email_hash = HMAC-SHA256(normalize(email), EMAIL_HMAC_KEY)` for lookups; `email_encrypted = key_id ‖ nonce ‖ AES-GCM(EMAIL_ENC_KEY, …)` for display. Keys are 32-byte base64 from env; fail fast if missing.
- `/auth/login` and `/auth/register` are rate-limited; keep them on the `authG` group in the router.
- Security headers middleware emits HSTS only when `COOKIE_SECURE=true`.
- Never log `email`, `password`, or session tokens. The redaction list lives in the logger middleware — add new sensitive field names there when introducing any.

## Testing

- Go unit tests colocate with packages (`*_test.go`).
- Integration tests spin up real Postgres via `testcontainers-go/postgres` in [api/internal/server/server_test.go](api/internal/server/server_test.go). When adding endpoints, extend this test to exercise at least one positive and one authz-negative case.
- Run everything with `make test` (Go + web). Go alone: `cd api && go test ./... -race`.
- Don't mock the DB in tests — we want real SQL behavior.

## Running the app

- `docker compose up -d --build` — full LAN stack on `http://localhost:3000` (web) and `http://localhost:8080` (api).
- `make dev-api` / `make dev-web` for local non-Docker dev.
- After any change that affects the API contract: `make gen`, then rebuild the containers whose code changed (`api`, `worker`, `web`).

## Scope boundaries (don't build these without asking)

Deferred from v1 — raise with the user before adding:

- Password reset via SMTP, OAuth, expense edits, multi-currency FX conversion, PWA offline, real-time sync, file receipts, audit log UI, pending-invite flow.
