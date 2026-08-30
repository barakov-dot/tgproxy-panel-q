# CLAUDE.md

Architecture decisions and conventions for `tgproxy-panel`. Full requirements live in
[plan.md](plan.md) (the original ТЗ, in Russian) — this file is the condensed, living
reference for implementation. Read plan.md first for *why*; this file is *how*.

## What this project is

A single Go binary (HTTP panel + Telegram bot, one process, systemd-managed) that issues
and revokes proxy access for an **already-installed** `tproxy-server`. It never touches
tproxy-server's relay logic, MTProxy, or `config.json`. Its entire write surface on the
target host is: `profiles.json`, one line in `Caddyfile`, and `systemctl restart
tproxy-server` / `systemctl restart caddy` — restart, not reload: confirmed against a real
install that tproxy-server's own `deploy/caddy.service` unit defines no `ExecReload=`, so
`systemctl reload caddy` fails outright ("Job type reload is not applicable"), and the
Caddyfile's `admin off` rules out `caddy reload`'s CLI too (it needs the admin API, which is
deliberately disabled). See `deploy/install.sh`'s own comment at the restart call for detail.

## Verified facts about tproxy-server (do not re-derive, do not guess)

Pulled from `telegramdesktop/tproxy-server@master` on 2026-08-30 (deploy/Caddyfile,
deploy/install.sh, deploy/tproxy-server.service, config.example.json,
profiles.example.json, README.md). Re-check upstream if these ever look inconsistent with
a real target host, but do not silently assume they've changed.

- **`profiles.json` is the source of truth on disk**, at `/etc/tproxy-server/profiles.json`,
  owner `root:tproxy`, mode `0400`. The running relay actually reads
  `/run/credentials/tproxy-server.service/profiles.json` — a copy systemd's
  `LoadCredential=` makes from the source file *at service start*, root-readable-only
  before the copy. **This is why a restart is mandatory**: there is no live reload,
  because the runtime copy is fixed at process start. Our applier writes the source path
  and always restarts; never write to `/run/credentials/...` directly.
- `/etc/tproxy-server/` itself is `root:tproxy 0750`; `config.json` is `root:tproxy 0640`.
  Our apply script must re-assert `chown root:tproxy` + `chmod 0400` on `profiles.json`
  after every write (a plain temp-file+rename can silently inherit the temp file's mode).
- **`internal/applier`'s own pre-flight `-check` call (before it ever hands off to sudo
  apply-profiles.sh) runs as the unprivileged `tgproxy-panel` process and needs to *read*
  `config.json`** for that — confirmed against a real install: without `tgproxy-panel` in
  `config.json`'s owning group (normally `tproxy`), this fails with "permission denied"
  and every issue/approve breaks, even though apply-profiles.sh's own root-context
  validation would have been fine. `deploy/install.sh` handles this by adding
  `tgproxy-panel` to that group when creating the user. This grants read access to a
  secrets-free settings file only — it does not and must not grant any access to the real
  `profiles.json` (`0400`, no group bits at all), which the panel process still never
  reads directly (see the declarative, DB-is-source-of-truth design below).
- **`deploy/tgproxy-panel.service`'s `ProtectSystem=strict` makes the whole filesystem
  read-only at the mount/namespace level for every process in the unit's tree — including
  a root-escalated `sudo apply-profiles.sh` child.** `sudo` changes UID/GID, not the mount
  namespace, so even genuinely running as root, that child still can't write through a
  read-only bind mount without an explicit `ReadWritePaths=` grant. Confirmed against a
  real install: without `/etc/tproxy-server` (profiles.json's directory) in
  `ReadWritePaths=`, `apply-profiles.sh`'s `mktemp` call fails with "Read-only file
  system" even though `-check` validation and the `sudo` escalation itself both succeeded.
  `deploy/install.sh` templates the real profiles.json directory into `ReadWritePaths=`
  alongside `data/`/`backup/`. This is orthogonal to the config.json group-read fact
  above: that one is a DAC (owner/group/mode) restriction, unaffected by `ReadWritePaths`;
  this one is a mount-level restriction, unaffected by DAC/UID.
- **The tproxy-server binary validates its own config+profiles**: `tproxy-server -config
  /etc/tproxy-server/config.json -profiles-file /etc/tproxy-server/profiles.json -check`.
  Shell out to this instead of hand-rolling profile-schema validation — it's the actual
  parser the relay uses. We still check `name`/`secret` uniqueness ourselves before
  writing, since `-check` isn't guaranteed to catch application-level dupes.
- Health/readiness: `curl --fail http://127.0.0.1:8081/healthz` and `.../readyz`, both on
  the loopback `admin_listen` from `config.json`. Poll `/readyz` after restart (install.sh
  itself polls 20× with 1s sleep) before flipping a user's status to `active`/`revoked` —
  if it never becomes ready, restore the previous `profiles.json` from backup and surface
  a clear error instead of leaving the DB and on-disk state inconsistent.
- **Caddyfile has no per-path routing yet** — the stock site block is a single
  `reverse_proxy 127.0.0.1:8080 { ... }` plus `encode zstd gzip`, a `header
  Strict-Transport-Security` line, and a `handle_errors { ... }` block with a strict CSP.
  Our installer must patch this into two `handle` blocks — the secret-path one first,
  matched to `/<PANEL_PATH_TOKEN>/*` and reverse-proxying to `127.0.0.1:9000`, then a
  bare `handle { ... }` wrapping the *existing* `reverse_proxy 127.0.0.1:8080 { ... }` as
  the catch-all fallback — and must preserve `encode`, the HSTS header, and
  `handle_errors` byte-for-byte. Always back up `/etc/caddy/Caddyfile` before touching it,
  run `caddy validate --config ... --adapter caddyfile` after, and roll back the backup if
  validation fails. Reference copy fetched during planning is in this session's scratch
  dir if needed again; re-fetch from
  `https://raw.githubusercontent.com/telegramdesktop/tproxy-server/master/deploy/Caddyfile`
  rather than trusting a stale local copy once install.sh is actually being written.
- `profiles.example.json` schema: `{"profiles":[{"name","secret","backend","carrier_mode"}]}`.
  `secret` is 32 lowercase hex chars. Multiple profiles can and normally do share one
  `backend` (one shared MTProxy instance) — never allocate a new backend port per user.

## Stack

- Go, no cgo (`modernc.org/sqlite`, not `mattn/go-sqlite3`) — cross-compiles cleanly from
  macOS to `linux/amd64` with just `GOOS=linux GOARCH=amd64 go build`.
- `github.com/go-chi/chi/v5` for HTTP routing.
- `html/template` + htmx + Tailwind (CDN `<script>`, no Node build step) for the panel UI.
  Assets embedded via `go:embed`. Follow the `frontend-design` skill's principles for a
  non-generic look — this is a small internal tool, not an excuse for default Bootstrap
  chrome, but don't over-invest either.
- `github.com/go-telegram-bot-api/telegram-bot-api/v5` for the bot (long polling, inline
  keyboards). Chosen over `mymmrac/telego` for maturity/API simplicity; revisit only if it
  turns out to lack something we need.
- `github.com/skip2/go-qrcode` for QR generation.
- `golang.org/x/crypto/bcrypt` for the admin password hash.
- Session cookies: hand-rolled HMAC-SHA256 signed cookie in `internal/auth`, not a
  dependency — the format is trivial (base64 payload + base64 HMAC) and one more dep
  isn't worth it for this.
- `log/slog`, JSON handler by default (`LOG_FORMAT=json`), text handler for local dev.
  **Never log secrets** (profile `secret`, bot token, session cookie value, admin
  password/hash) — log profile `name` and `telegram_id`, not the secret itself.

## Directory structure

```
cmd/tgproxy-panel/       main.go — flag/env parsing, wiring, graceful shutdown of
                          HTTP server + bot goroutine together
internal/
  config/                env loading + validation into a typed Config struct
  logging/                slog setup (json/text)
  models/                 User, Settings, AuditLog types shared across packages
  store/                  SQLite access (modernc.org/sqlite), schema.sql embedded,
                          CREATE TABLE IF NOT EXISTS on startup (no migration
                          framework — schema is small and stable, see plan.md §4)
  secretgen/              random secret (32 hex), profile name, path-token generation
  applier/                profiles.json read/backup/patch/validate/write/restart/
                          healthcheck/rollback — the only package that shells out to
                          sudo/systemctl/curl on the target host
  auth/                   login, bcrypt check, signed session cookies, rate limiting
  httpserver/              chi router + handlers; templates/ and static/ embedded
  bot/                     telegram-bot-api long-polling handlers, admin notification,
                          approve/deny inline keyboard callbacks
  qrcode/                  thin wrapper around skip2/go-qrcode sized for chat + panel use
deploy/
  install.sh               interactive installer (see plan.md §8)
  tgproxy-panel.service     systemd unit for the panel itself
  apply-profiles.sh         root-owned script the panel sudo-invokes for the actual
                          profiles.json write + restart (plan.md §7 privilege split)
  sudoers.tgproxy-panel     NOPASSWD rule scoped to exactly that one script
.github/workflows/release.yml   build linux/amd64 binary on tag push, attach to Release
```

`internal/applier` is the only package allowed to call `exec.Command` for
`sudo`/`systemctl`/`curl`/`caddy validate` — keep privileged-operation surface area in one
place, auditable in one file/small set of files.

## Conventions

- Business logic returns `error`; `internal/httpserver` and `internal/bot` translate
  errors to user-facing messages (Russian, matching plan.md's UI copy) at the edge, not
  deeper in the stack.
- Every state-changing operation (issue, revoke, approve, deny) goes through
  `internal/applier` and is recorded as an audit row — panel and bot are both thin
  callers into the same service layer, never duplicate the issue/revoke logic between
  them (plan.md §6 explicitly requires the bot's approve/deny to reuse panel logic).
  Concretely: a small `internal/service` (or equivalent) package owns "issue profile for
  user X", "revoke profile for user X", "approve/deny pending request" as the single
  entry points; httpserver and bot handlers call into it, never touch store/applier
  directly for these operations.
- One user telegram_id has at most one non-revoked profile at a time — enforce this at
  the service layer before ever calling into applier, not just as a DB constraint.
- No secrets in the public repo, ever — `.env.example` only, real values stay in
  `.env` (gitignored). Before any commit, sanity check for accidentally-embedded
  tokens/passwords.
- Default to writing no comments; the one exception already used in this file's
  "Verified facts" section — non-obvious upstream behavior (systemd credential timing,
  why `-check` exists) is exactly the kind of thing worth a comment at the call site too.

## Build / test loop (macOS dev machine)

```bash
go build ./...            # darwin target, fast local iteration
go vet ./...
go test ./...
GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/tgproxy-panel-linux ./cmd/tgproxy-panel
```

The last command is the actual release artifact shape — run it before considering any
stage "done", since a darwin-only build passing tells you nothing about cgo creeping in
via a transitive dependency.

## Commit/stage plan

Roughly one commit (or small commit group) per stage, in dependency order:

1. Scaffold: go.mod, cmd/ placeholder, README skeleton, this file. *(this commit)*
2. `internal/config`, `internal/logging`, `internal/models`, `internal/store` (schema +
   CRUD for users/settings).
3. `internal/secretgen`, `internal/applier` (profiles.json + Caddyfile logic, no server
   access to test against — write it defensively per the verified facts above, add unit
   tests around JSON patching/validation with fixture files).
4. `internal/auth` (session cookies, login, rate limit).
5. `internal/httpserver` (routes, templates, htmx list/detail/settings pages) and
   `internal/bot` (long polling, inline keyboards) — independent of each other, both
   depend on 2–4, can be built in parallel.
6. `internal/service` (issue/revoke/approve/deny orchestration) wiring httpserver + bot
   to the same logic; `cmd/tgproxy-panel/main.go` real wiring + graceful shutdown.
7. `deploy/install.sh`, `tgproxy-panel.service`, `apply-profiles.sh`, `sudoers.*`.
8. `.github/workflows/release.yml`.
9. `README.md` real install instructions (mirroring tproxy-server's README structure).
10. Pass over all of the above: `go vet`, `go test`, linux cross-build, and a read-through
    against plan.md §13's decision table to confirm nothing was missed.
