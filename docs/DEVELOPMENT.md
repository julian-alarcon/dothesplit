# Development guide

How to check, build, test, and deploy DoTheSplit. See [../BLUEPRINT.md](../BLUEPRINT.md) for product scope and [../CLAUDE.md](../CLAUDE.md) for the conventions we enforce when editing the code.

## Prerequisites

- **Docker** + **Docker Compose v2** (only strict requirement for running the stack)
- **Go 1.25+** (for local dev and unit tests outside Docker)
- **Node 24+** and **npm 10+** (for local dev and `astro check`)
- `make`, `openssl` (for key generation), `curl` and `python3` (used in smoke scripts)

## First-time setup

```bash
cp .env.example .env
echo "EMAIL_ENC_KEY=$(openssl rand -base64 32)"   >> .env
echo "EMAIL_HMAC_KEY=$(openssl rand -base64 32)"  >> .env
echo "PASSWORD_PEPPER=$(openssl rand -base64 32)" >> .env

docker compose up -d --build
```

Web is served at <http://localhost:3000>. API at <http://localhost:8080>. Health probes: `/healthz`, `/readyz`.

## The contract-first workflow

Any change that touches the HTTP surface goes through the same loop:

1. Edit [docs/openapi.yaml](openapi.yaml) first.
2. `make gen` - regenerates Go types (`api/internal/apigen/`) and TypeScript types (`web/src/lib/api/schema.d.ts`). The build won't compile until your code matches.
3. If the DB schema changes, add a migration under [api/migrations/](../api/migrations/) - `NNNN_*.up.sql` + a matching `.down.sql`. Migrations are append-only.
4. Backend order: **repo → service → handlers → router**.
5. Frontend: rebuild pages against the new types, add/adjust Astro pages and SSR API routes under `web/src/pages/api/`.
6. Tests, rebuild containers, smoke.

## Check (fast, no containers needed)

```bash
make gen            # regenerate Go + TS types from docs/openapi.yaml
cd api && go vet ./...
cd api && go build ./...
cd web && npm run check    # astro check (TypeScript + Astro diagnostics)
cd web && npm run build    # astro build (also catches CSP-bundling issues)
```

`astro check` / `astro build` should produce 0 errors. One harmless hint (`ts(7027) unreachable code`) may appear on DOM event-handler scripts - it's a known TypeScript quirk, not a regression.

## Test

**Go**: unit + integration. The integration tests use [testcontainers-go](https://golang.testcontainers.org/), so a running Docker daemon is required. Each test run spins up its own short-lived Postgres container, applies all migrations, and tears it down.

```bash
make test-go                         # all Go tests
cd api && go test ./... -race        # same thing, one level down
cd api && go test ./internal/server/ -run TestGoldenPath -v    # one test
```

The E2E suite in [api/internal/server/server_test.go](../api/internal/server/server_test.go) covers the full golden path (register, login, group, members, expense split modes, balances, settlements, soft-delete, category + revision log, payer swap, logout).

**Web**: two layers.

- Unit (vitest, jsdom) under `web/src/**/*.test.ts`. Pure helpers only - the canvas-touching avatar pipeline isn't exercised here, only its color math. Run with `cd web && npm test` (or `npm run test:watch`).
- E2E (Playwright, Chromium) under `web/tests/e2e/`. Requires the full docker stack already running and the install token from `docker compose logs api`:

  ```bash
  docker compose up -d --build
  TOKEN=$(docker compose logs api | grep -oE 'token=[A-Za-z0-9_-]+' | head -1 | cut -d= -f2)
  cd web && SETUP_TOKEN=$TOKEN npm run test:e2e
  ```

  CI runs the same flow on every PR; locally it's optional, useful when changing SSR-to-API wiring.

## Build the container images

For local dev, build via compose. **Production deployments should pull pinned images from GHCR** (see "Releasing" below) - never build from `main` on a deployment host.

```bash
docker compose build                 # build all three images (api, web, worker shares api)
docker compose build api             # rebuild just one
make up                              # rebuild + start, stamping BUILD_COMMIT + BUILD_VERSION
```

Images:

- `dothesplit-api` - multi-stage Go build → distroless final stage (serves `/api` and the `/worker` command). `-ldflags` stamps `main.version` and `main.commit` from the build args, surfaced at `/healthz`.
- `dothesplit-web` - multi-stage Node 24 build → Astro SSR standalone server. `BUILD_COMMIT` + `BUILD_VERSION` reach the SSR runtime as `process.env.*` and feed the page footer.

The `make up` target reads the top-level `VERSION` file (release-please-managed) for `BUILD_VERSION` and the current git short SHA for `BUILD_COMMIT`.

## Releasing

Releases are automated by [release-please](https://github.com/googleapis/release-please-action) on every push to `main`. You don't run release commands by hand - you write conventional commits and merge the Release PR when it looks right.

### The flow

1. **Land changes on `main`** with [conventional commit titles](https://www.conventionalcommits.org). Commit type drives the bump:

   | Type                       | Bump  | Example                                            |
   | -------------------------- | ----- | -------------------------------------------------- |
   | `fix:`                     | patch | `fix(api): reject empty currency on group create`  |
   | `feat:`                    | minor | `feat(web): currency picker flag glyphs`           |
   | `feat!:` / `BREAKING CHANGE` footer | major | `feat(api)!: drop /v1/legacy/expenses` |
   | `chore:`, `docs:`, `style:`, `test:`, `ci:`, `refactor:` | none | (still appears in CHANGELOG under their section)   |

2. **release-please opens (or updates) a Release PR** named `chore(main): release X.Y.Z`. It bumps `web/package.json` (the single version source of truth) and regenerates `CHANGELOG.md`. Review it like any other PR. If you don't like the proposed version, override via a commit footer (`Release-As: 1.0.0`) and push - the PR will rewrite itself.

3. **Merging the Release PR** auto-creates the git tag `vX.Y.Z` and a GitHub Release with the changelog body.

4. **The tag triggers two workflows in parallel**:
   - [`publish.yml`](../.github/workflows/publish.yml) builds multi-arch (`linux/amd64,linux/arm64`) images for `api` and `web`, pushes to `ghcr.io/julian-alarcon/dothesplit-{api,web}` with tags `:vX.Y.Z`, `:vX.Y`, `:vX`, `:latest`, plus a build provenance attestation.
   - [`compliance.yml`](../.github/workflows/compliance.yml) regenerates SBOMs + `THIRD_PARTY_LICENSES.md` and attaches them to the GitHub Release.

5. **Every push to `main`** (including merges that are not the Release PR) also triggers `publish.yml`, which pushes `:dev`, `:main`, and `:sha-<short>` tags. The `:dev` tag tracks the latest `main` and is appropriate for a staging environment.

### Where the version surfaces

| Location                                  | Source                                                |
| ----------------------------------------- | ----------------------------------------------------- |
| `web/package.json` `version`              | release-please bump on merge (single source of truth) |
| GitHub Release page                       | release-please on PR merge                            |
| `ghcr.io/.../dothesplit-{api,web}:vX.Y.Z` | `publish.yml` on tag                                  |
| API `GET /healthz` JSON                   | `-ldflags` baked in by `api/Dockerfile`               |
| Web page footer                           | `BUILD_VERSION` env baked in by `web/Dockerfile`      |

### Emergency manual release

Only when release-please is broken or the queued Release PR can't be merged in time:

```bash
# 1. Bump web/package.json + .release-please-manifest.json BY HAND, commit.
# 2. Tag and push.
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

The tag still triggers `publish.yml` and `compliance.yml`. The CHANGELOG entry won't be auto-generated, so write it manually in the GitHub Release UI. **Do not push manual version bumps to `main` outside a release-please PR** - it desyncs the manifest and the next automated PR will produce a wrong version.

## Run

```bash
docker compose up -d                 # start/resume the stack
docker compose up -d --build         # rebuild only stale images, then start
docker compose up -d --build web     # rebuild + restart just the web service
docker compose logs -f api           # follow api logs
docker compose ps                    # service status
docker compose down                  # stop (keeps the Postgres volume)
docker compose down -v               # stop AND destroy the Postgres volume
```

### Services

| Service    | Image               | Purpose                                           |
| ---------- | ------------------- | ------------------------------------------------- |
| `postgres` | `postgres:18-alpine`| Database; mounted at `/var/lib/postgresql`        |
| `migrate`  | `migrate/migrate`   | One-shot; runs all `*.up.sql` and exits           |
| `api`      | `dothesplit-api`    | HTTP API on `:8080`, session cookies              |
| `worker`   | `dothesplit-api`    | Same image, runs `/worker` - materializes recurring expenses |
| `web`      | `dothesplit-web`    | Astro SSR on `:3000`                              |

## Smoke test the running stack

```bash
# Stack health
curl -fsS http://localhost:8080/healthz     # 200 ok
curl -fsS http://localhost:8080/readyz      # 200 once DB is reachable

# End-to-end user flow
JAR=/tmp/c
curl -sS -c $JAR -X POST http://localhost:8080/v1/auth/register \
  -H 'content-type: application/json' \
  -d '{"email":"dev@test.dev","password":"password12","display_name":"Dev"}'

curl -sS -b $JAR http://localhost:8080/v1/me
curl -sS -b $JAR http://localhost:8080/v1/categories | python3 -m json.tool
```

Then open <http://localhost:3000/login>, log in with the credentials you just created, and walk through create-group → add-expense → edit-expense.

## Deploy (LAN / TrueNAS)

This stack ships HTTP-only by default for LAN use on TrueNAS. For anything internet-facing, terminate TLS at an upstream reverse proxy (Caddy, Traefik, Cloudflare Tunnel) and flip the cookie flags.

```bash
# One-time, on the host
cp .env.example .env
echo "EMAIL_ENC_KEY=$(openssl rand -base64 32)"    >> .env
echo "EMAIL_HMAC_KEY=$(openssl rand -base64 32)"   >> .env
echo "PASSWORD_PEPPER=$(openssl rand -base64 32)"  >> .env
echo "POSTGRES_PASSWORD=$(openssl rand -base64 24)" >> .env
# Update DATABASE_URL in .env so the password matches POSTGRES_PASSWORD.

# For HTTPS deployments
echo "COOKIE_SECURE=true"                          >> .env
echo "WEB_ORIGIN=https://split.yourdomain.tld"     >> .env

docker compose up -d --build
```

When `COOKIE_SECURE=true` the session cookie is renamed to `__Host-dts_session` (browsers reject the `__Host-` prefix without `Secure`). When `false`, it's the plain `dts_session`. The backend picks the right name automatically.

### What the three keys do

The three `EMAIL_ENC_KEY` / `EMAIL_HMAC_KEY` / `PASSWORD_PEPPER` values are not config knobs - they're the cryptographic material the database is built around. Generate them once on first install and back them up; if you lose them the data is unrecoverable, and if they leak an attacker can decrypt every email and crack every password offline.

All three are 32 raw bytes, base64-encoded for transport. `openssl rand -base64 32` produces exactly that.

#### `EMAIL_ENC_KEY` - emails at rest

Code: [api/internal/crypto/email.go](../api/internal/crypto/email.go).

Every email address goes into the `users.email_encrypted` column as `key_id ‖ nonce ‖ AES-GCM(EMAIL_ENC_KEY, plaintext)`:

- **`key_id`** is a one-byte tag (currently `0x01`) that lets you rotate to a new key later without losing access to rows encrypted under the old one.
- **`nonce`** is 12 random bytes generated per row - required for AES-GCM, and the reason two users with the same email get two different ciphertexts.
- **AES-GCM** is authenticated encryption: the auth tag is appended after the ciphertext, so any tampering with the row (or with `key_id` / `nonce`) makes decryption fail rather than producing garbage plaintext.

The plaintext is only kept in memory for the duration of a request (e.g. when rendering an email template, when an admin views the user detail page, or when the SMTP outbox dispatcher mails it). Logs explicitly redact email fields ([api/internal/middleware/logging.go](../api/internal/middleware/logging.go)).

#### `EMAIL_HMAC_KEY` - login lookups without storing the address

You can't query "user with email X" against an AES-GCM column - every row has a different nonce, so ciphertexts don't match even when plaintexts do. We store a *separate* deterministic fingerprint in `users.email_hash`:

```
email_hash = HMAC-SHA256(EMAIL_HMAC_KEY, normalize(email))
```

`normalize` lower-cases and trims (see `EmailCipher.HashEmail`). The HMAC is keyed, so an attacker who steals the database without the key can't brute-force the (small, finite) email space against `users.email_hash` - they have to break HMAC-SHA256 first.

Login, register-conflict-detection, password-reset and "is this email already on file" all hash the input email and look it up by `email_hash`. The encrypted column is decrypted only after that lookup succeeds.

Splitting the two keys is deliberate: it means a leak of `EMAIL_HMAC_KEY` lets an attacker test whether *specific* emails are registered (still bad), but they still can't read any email plaintext without `EMAIL_ENC_KEY`. And vice-versa.

#### `PASSWORD_PEPPER` - server-side secret added to password hashes

Code: [api/internal/crypto/password.go](../api/internal/crypto/password.go).

Passwords are hashed with Argon2id (memory-hard, GPU-resistant), but Argon2id alone protects against an attacker with the database *and* nothing else. If they also walk away with the binary they can run dictionary attacks at full speed against the salted hashes. The pepper closes that gap:

```
hash = Argon2id(password ‖ PASSWORD_PEPPER, salt, params)
```

The pepper is stored only in the env var - never in the database. So an attacker who exfiltrates `users.password_hash` and the salts but not the pepper can't even start cracking; they're missing 32 bytes of unguessable entropy that get mixed into every hash. The pepper is used at register, login, and `/me/password` change.

Salt + pepper + Argon2id is a three-part defense (per-user randomness, server-secret randomness, slow KDF). Take any one away and the others get weaker.

#### Rotation, when you'd actually do it

Today there's no rotation tool - that's a deliberate v1 cut. If a key leaks, the recovery is "mint a new key, dump and re-encrypt every affected row, then deploy with the new key." The `key_id` byte in the email ciphertext exists so a future rotation tool can read the old key for old rows and the new key for new rows during the cutover. None of that exists yet - if you suspect a key has leaked, the safe path today is to take the instance down, restore from a clean snapshot, rotate the key, and have users reset passwords.

### Updating a running deployment

For production deployments, **pull pinned images from GHCR** instead of building from `main` on the host. The release pipeline already built and signed them.

```bash
# On the deployment host, after a new release is tagged:
docker compose pull                  # pull the pinned :vX.Y.Z (or :latest) images
docker compose up -d                 # restart services with the new images
```

Migrations run automatically via the `migrate` one-shot on every `up`.

For staging, point compose at the `:dev` tag (which tracks `main`) and pull on a schedule, or wire a webhook to your registry. For dev hosts where you want to reflect uncommitted local changes, the legacy build-from-source path still works:

```bash
git pull
docker compose up -d --build         # rebuild only changed services
```

But never do that on a release-tracking deployment - it desyncs from the version stamps in the image and breaks the "what's running?" answer at `/healthz`.

### Major Postgres upgrades

Postgres's on-disk format is not compatible across major versions. Bumping the image tag alone will not work - the container will refuse to start and log "there appears to be PostgreSQL data in /var/lib/postgresql/data (unused mount/volume)" or similar.

Two paths:

1. **Dev / LAN with no data you care about**: `docker compose down -v` (destroys the volume), then `docker compose up -d --build`. Migrations recreate the schema from scratch; all app data is lost.
2. **Preserving data**: `pg_dump` from the old container, `docker compose down -v`, bring up the new image, restore with `psql` or `pg_restore`.

The compose file mounts the volume at `/var/lib/postgresql` (not `/var/lib/postgresql/data`) because PG 18's official image expects the parent directory so `pg_upgrade --link` can work in place across majors. Don't change this.

## Troubleshooting

### `process` is undefined in Astro SSR routes

Our SSR handlers under `web/src/pages/api/*.ts` read `process.env.API_BASE_URL_INTERNAL`. Astro's `import.meta.env` only exposes variables prefixed with `PUBLIC_`, so anything server-only must use `process.env`. This also means `@types/node` is a required dev dependency (`astro check` needs it).

### Login form does nothing / redirects back to /login

Almost always a cookie problem. The session cookie name switches between `dts_session` (HTTP) and `__Host-dts_session` (HTTPS). If you flip `COOKIE_SECURE` but the frontend middleware is still looking for the old name, middleware won't find the session. Both sides should already handle this transparently - if not, grep for `dts_session` in `api/internal/middleware/` and `web/src/middleware.ts`.

### A form control doesn't "do" anything (e.g. the category picker doesn't close on select)

CSP is blocking an inline script. We have `security.csp: true` enabled in [../web/astro.config.mjs](../web/astro.config.mjs). Any client-side JS must live in a real module under `web/src/scripts/` and be imported from a `<script>` tag - not written as `<script is:inline>` or a raw inline block inside an `.astro` page. External bundled scripts are covered by `script-src 'self'`; inline scripts need per-hash allowlisting that's brittle across build/serve paths.

### `astro check` complains that it can't find `process`

Run `npm install --save-dev @types/node` in `/web`. This started being required with Astro 6.

### Test container fails to pull Postgres image

`testcontainers-go` uses the same Docker daemon as compose. If compose works, tests will too. If not: `docker info` and `docker pull postgres:18-alpine` to prime the image cache.

### Shiki warning during `astro build` about CSP

> "Shiki syntax highlighting uses inline styles that are not compatible with Content Security Policy"

Harmless for us - we don't render Markdown code blocks anywhere. Ignore.

## Useful targets

Run `make help` for the full list. The ones you'll actually reach for:

| Target            | What it does                                                       |
| ----------------- | ------------------------------------------------------------------ |
| `make gen`        | Regenerate Go + TS API bindings from `docs/openapi.yaml`           |
| `make migrate-up` | Apply all pending migrations                                       |
| `make test-go`    | Full Go test suite (unit + integration via testcontainers)         |
| `make dev-api`    | Run the Go API locally against Docker Postgres                     |
| `make dev-web`    | Run Astro dev server                                               |
| `make build`      | Build Go binaries (`bin/api`, `bin/worker`)                        |
| `make up`         | `docker compose up -d --build`, baking current SHA in              |
| `make compliance` | Regenerate `THIRD_PARTY_LICENSES.md` + CycloneDX SBOMs into `sbom/` |

**`make up`** computes `BUILD_COMMIT=$(git rev-parse --short HEAD)` and `BUILD_VERSION=$(cat VERSION)` and passes both to every Dockerfile as build args. The web image gets them in `process.env.*` and the shared [`Base.astro`](../web/src/layouts/Base.astro) layout renders a footer with the version (linking to the GitHub Release) and the commit (linking to the commit page). The api/worker binary gets them via `-ldflags` and surfaces them at `GET /healthz`. When building outside a git checkout (`docker compose build` directly, a tarball, etc.), both default to `dev` and the surfaces show `dev` with no links.
