# Architecture & conventions

Living document for **tgproxy-panel-q**. Full product spec: [PLAN.md](PLAN.md).

Reference copies of upstream tproxy-server files (for Caddyfile patcher / profiles format):
[docs/reference/](docs/reference/).

---

## System boundary

| Component | Owned by panel | Notes |
|-----------|----------------|-------|
| `/etc/tproxy-server/profiles.json` | **write** (via sudo script) | Panel adds `user_<id>` entries; pre-existing profiles (e.g. `default`) are preserved |
| `/etc/tproxy-server/config.json` | read-only | Global limits; never modified |
| `/etc/caddy/Caddyfile` | patch once at install | Adds secret path → `:9000` |
| `tproxy-server` systemd unit | restart only | No live reload; restarts drop all sessions |
| `mtproxy.env` (`/etc/mtproxy/mtproxy.env`) | **write** (via sudo script) | Workers/limits + one `MTPROXY_SECRET` (default profile) |
| `mtproxy.secrets` (`/etc/mtproxy/mtproxy.secrets`) | **write** (via sudo script) | One secret per line → separate `-S` flags on MTProxy |
| SQLite `panel.db` | full ownership | Users, settings, audit |

Panel does **not** spawn per-user MTProxy processes. Every issued profile points at the
same loopback backend (default `127.0.0.1:2398`, configurable via `TPROXY_BACKEND`).
`apply-profiles.sh` syncs all profile secrets to the single official MTProxy via
multiple `-S` flags (`deploy/mtproxy-exec.sh` wrapper).

---

## Process model

Single Go binary (`cmd/tgproxy-panel`), one systemd service:

```
main
 ├── config.Load()          # .env + validation
 ├── store.Open()           # SQLite migrate
 ├── service.New()          # business logic (issue/revoke/approve)
 ├── applier.New()          # profiles.json apply pipeline
 ├── httpserver.New()       # chi router, :9000, htmx UI
 └── bot.Run()              # long polling goroutine
```

Graceful shutdown: cancel context → stop HTTP → bot stops → close DB.

---

## Upstream file formats (verified)

### profiles.json

From `profiles.example.json` — each profile has four fields:

```json
{
  "profiles": [
    {
      "name": "user_123456789",
      "secret": "0123456789abcdef0123456789abcdef",
      "backend": "127.0.0.1:2398",
      "carrier_mode": "https"
    }
  ]
}
```

- `name`: `user_<telegram_id>` (stable, unique)
- `secret`: 32 lowercase hex chars (`secretgen` package)
- `backend`: from `TPROXY_BACKEND` env (same for all users)
- `carrier_mode`: from `TPROXY_CARRIER_MODE` env (default `https`)

Validation before apply: valid JSON, unique `name`/`secret`, optional
`tproxy-server -check` if binary available.

On production installs, systemd `LoadCredential` may expose the file at
`/run/credentials/tproxy-server.service/profiles.json` while the canonical path
remains `/etc/tproxy-server/profiles.json`. The apply script writes the canonical
path; tproxy-server reads the credential symlink.

File mode after write: **0400**, preserve owner/group from existing file (typically
`root:root`).

### Caddyfile (upstream default)

Real upstream site block (see `docs/reference/tproxy-server-Caddyfile`) is a **flat**
`reverse_proxy 127.0.0.1:8080` with `encode`, HSTS, and `handle_errors` — no `handle`
directives yet.

**Install-time patch strategy:** inside `{$TPROXY_HOSTNAME} { ... }`, insert **before**
the existing `reverse_proxy 127.0.0.1:8080` block:

```caddyfile
handle /<PANEL_PATH_TOKEN>/* {
    reverse_proxy 127.0.0.1:9000
}
handle {
    reverse_proxy 127.0.0.1:8080 {
        transport http {
            response_header_timeout 40s
        }
    }
}
```

Then remove/replace the original bare `reverse_proxy 127.0.0.1:8080 { ... }` so traffic
is routed through the two `handle` blocks. Keep `encode`, `header`, and `handle_errors`
directives outside/unmodified.

Validation flow: backup → patch → `caddy validate --config ...` → `systemctl restart caddy`
(on failure, restore backup). Use `systemctl restart`, not `reload`, if the site block
structure changes materially.

### Health check after tproxy restart

1. `systemctl is-active tproxy-server` (timeout ~30s)
2. `curl --fail http://127.0.0.1:8081/readyz` (from `config.json` `admin_listen`)

If health check fails: restore `profiles.json` from latest backup, do **not** commit
user status change to `active`/`revoked`.

---

## Privileged operations (sudoers model)

Default (recommended): service runs as `tgproxy-panel` system user.

```
tgproxy-panel ALL=(root) NOPASSWD: /opt/tgproxy-panel/bin/apply-profiles.sh
```

`apply-profiles.sh` (root-owned, 0755):

1. Validate candidate JSON path argument
2. Merge candidate (panel-managed `user_<telegram_id>` profiles only) with the **live**
   profiles.json: keep every non-panel profile unchanged (e.g. upstream `default`),
   replace all panel-managed entries with the candidate list
3. Backup current profiles → `$BACKUP_DIR/profiles.json.<UTC>.bak`
4. Sync MTProxy secrets from live `$TPROXY_PROFILES_PATH` → `/etc/mtproxy/mtproxy.secrets` (one per line, each becomes `-S`); keep a single `MTPROXY_SECRET` in `mtproxy.env`; restart `mtproxy` if changed (after tproxy-server restart succeeds)
5. Rotate backups (keep last `BACKUP_KEEP`)
6. Atomic install to `$TPROXY_PROFILES_PATH` with correct owner/mode
7. `systemctl restart tproxy-server`
8. Exit non-zero if restart fails

Go `internal/applier` writes candidate to temp file, invokes
`sudo $APPLY_PROFILES_SCRIPT <path>`.

Optional `RUN_AS_ROOT=1` env bypasses sudo for dev/simplified installs.

---

## Package layout

```
cmd/tgproxy-panel/          # main, -hash-password helper
internal/
  config/                   # .env loading, defaults, validation
  store/                    # SQLite CRUD, migrations (embed schema.sql)
  models/                   # User, Setting, Audit structs + status constants
  secretgen/                # 32-hex secret, profile name, path token
  logging/                  # structured json/text slog wrapper
  applier/                  # desired state → profiles.json apply + rollback
  service/                  # Issue, Revoke, Approve, Deny, SendToUser
  auth/                     # bcrypt login, HMAC session cookie, rate limit
  httpserver/               # chi routes, handlers, html/template + htmx
  bot/                      # telegram long polling, inline keyboards
  qrcode/                   # PNG bytes for proxy link
deploy/
  install.sh
  apply-profiles.sh
  tgproxy-panel.service
  sudoers.tgproxy-panel
docs/reference/             # frozen upstream tproxy-server examples
```

**Dependency rule:** `service` orchestrates `store` + `applier` + `bot` sender;
`httpserver` and `bot` call `service` only (no direct applier from handlers).

---

## HTTP routing

Base path: `/<PANEL_PATH_TOKEN>/` (trailing slash normalized).

| Route | Auth | Purpose |
|-------|------|---------|
| `GET /login` | public | Login form |
| `POST /login` | public | Authenticate (rate-limited) |
| `POST /logout` | session | Clear cookie |
| `GET /` | session | Users list (htmx partials) |
| `GET /users` | session | Table partial (sort/filter) |
| `GET /users/{id}` | session | User detail |
| `POST /users/{id}/approve` | session | Approve pending |
| `POST /users/{id}/deny` | session | Deny pending |
| `POST /users/{id}/revoke` | session | Revoke active |
| `POST /users/{id}/send` | session | Bot DM link+QR |
| `GET /settings` | session | Settings page |
| `POST /settings/auto-issue` | session | Toggle auto issue |
| `GET /users/{id}/qr` | session | QR PNG |

Static assets embedded via `go:embed` under `internal/httpserver/static/`.
Templates under `internal/httpserver/templates/`.

Frontend: **htmx** + **Tailwind CDN** (no npm build). Design: clean admin dashboard,
not default Bootstrap — see PLAN.md §3.

---

## Auth & sessions

- Admin credentials: `ADMIN_LOGIN`, `ADMIN_PASSWORD_HASH` (bcrypt) in `.env`
- Session: HMAC-SHA256 signed cookie (`SESSION_SECRET`), HttpOnly, Secure (when behind HTTPS), SameSite=Lax
- Session TTL: 24h (configurable constant)
- Login rate limit: per-IP token bucket in memory (e.g. 5 attempts / 15 min)

CLI helper: `tgproxy-panel -hash-password` reads password from terminal, prints bcrypt hash.

---

## Telegram bot

Library: `go-telegram-bot-api/telegram-bot-api/v5`.

Flow:

- `/start` → welcome + inline "🔑 Получить прокси"
- Callback `get_proxy`:
  - active user → resend existing link + QR
  - auto_issue on → `service.Issue()`
  - auto_issue off → `service.Request()` + notify `ADMIN_TELEGRAM_ID`
- Admin callbacks `approve:{id}` / `deny:{id}` → same as panel actions

Message format: HTML (preferred over MarkdownV2 for fewer escape issues).

---

## Data model conventions

**User statuses:** `pending` | `active` | `revoked` | `denied`

- One `active` profile per `telegram_id` (enforced in service layer)
- Re-request while active: bot shows existing credentials, no new profile
- `revoked` / `denied`: profile removed from profiles.json (if was active/pending with profile)

**Settings keys:**

| Key | Values | Source |
|-----|--------|--------|
| `auto_issue` | `true` / `false` | `.env` seeds on first run; panel edits DB |

**Audit log:** every issue/revoke/approve/deny/login-fail (optional). Never log full secrets.

---

## Configuration

All runtime config from `.env` (see `.env.example`). Required fields validated at startup;
missing/invalid → fatal log + exit.

Build tags: none. **CGO_ENABLED=0** always (pure Go sqlite).

Logging: `LOG_FORMAT=json|text`, default `json` for production/systemd.

---

## Testing conventions

- Table-driven unit tests per package
- `store`: in-memory or temp-file SQLite
- `applier`: temp dirs, fake `systemctl` via injected runner interface
- `httpserver`: `httptest` + mocked service
- `bot`: handler logic tested with fake bot API
- Run: `CGO_ENABLED=0 go test ./...`

---

## CI / release

`.github/workflows/release.yml`:

- Trigger: tag `v*.*.*`
- Gate: `go build ./...`, `go vet`, `go test` with `CGO_ENABLED=0`
- Artifact: `tgproxy-panel-linux-amd64` (+ sha256)
- `deploy/install.sh` downloads from `releases/latest/download/tgproxy-panel-linux-amd64`

---

## Commit / implementation stages

Recommended incremental commits (each should pass `go test ./...`):

| # | Commit scope | Packages |
|---|--------------|----------|
| 1 | `chore: scaffold repo, docs, reference files` | root, docs/ |
| 2 | `feat(config): env loading and validation` | config/ |
| 3 | `feat(store): sqlite schema and user CRUD` | store/, models/ |
| 4 | `feat(secretgen,logging): utilities` | secretgen/, logging/ |
| 5 | `feat(auth): login, session, rate limit` | auth/ |
| 6 | `feat(applier): profiles apply pipeline` | applier/ |
| 7 | `feat(service): issue, revoke, approve business logic` | service/ |
| 8 | `feat(httpserver): panel UI and routes` | httpserver/, qrcode/ |
| 9 | `feat(bot): telegram bot handlers` | bot/ |
| 10 | `feat(main): wire and graceful shutdown` | cmd/ |
| 11 | `feat(deploy): install.sh, systemd, sudoers` | deploy/ |
| 12 | `docs: README install guide polish` | README.md |

---

## Naming & style

- Go 1.25+, standard `gofmt`
- Exported identifiers: Go idiomatic English
- Errors: wrap with `%w`, user-facing messages in Russian for bot/UI where PLAN specifies
- No secrets in logs, tests, or committed files
- Module path: `github.com/barakov-dot/tgproxy-panel-q`
