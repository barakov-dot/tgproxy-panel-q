# tgproxy-panel

Web panel + Telegram bot for issuing and revoking user access to an already-installed
[`tproxy-server`](https://github.com/telegramdesktop/tproxy-server) deployment (Telegram's
WEB proxy relay). It manages entries in tproxy-server's `profiles.json` and restarts the
service when they change — it never touches tproxy-server's own relay logic, MTProxy, or
`config.json`. Users request access through the bot; the admin approves (or auto-issues) from
either the bot or the web panel, and gets a shareable link and QR code back.

Requires a working tproxy-server installation on the same host. If you haven't installed that
yet, do it first from its own repository — this project only adds a user-management layer on
top.

## Requirements

- **tproxy-server already installed and running** on this host — Caddy, MTProxy, and
  `/etc/tproxy-server/{config,profiles}.json` all in place. See
  [`telegramdesktop/tproxy-server`](https://github.com/telegramdesktop/tproxy-server) if not.
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- Your own Telegram numeric user ID (to receive pending-request notifications as admin).
- The same **Debian 13 / Ubuntu 24, x86_64** host tproxy-server runs on.
- Root (or `sudo`) access on that host.
- A local Go toolchain is only needed as a fallback — the installer prefers downloading a
  prebuilt release binary, and falls back to `go build` if that download fails.

## Install

Clone the repository and run the installer as root:

```bash
git clone https://github.com/barakov-dot/tgproxy-panel.git
cd tgproxy-panel
sudo ./deploy/install.sh
```

This is the one supported install method. `deploy/install.sh` resolves its own location and
reads sibling files (`deploy/apply-profiles.sh`, `deploy/sudoers.tgproxy-panel`,
`deploy/tgproxy-panel.service`) relative to a real checkout — piping it straight from `curl`
into `bash` would leave those lookups pointing at nothing and fail partway through, so no
one-line `curl | bash` form is offered here. Always clone first, then run the script from
inside the clone.

The installer is interactive and asks for, in order:

1. **Install directory** (default `/opt/tgproxy-panel`) — where the panel's own binary,
   `.env`, database, and backups live.
2. **Paths to tproxy-server's `profiles.json` and `config.json`** — auto-detected at their
   default locations (`/etc/tproxy-server/{profiles,config}.json`) if a `tproxy-server`
   systemd service is found; otherwise you're prompted, with a warning if either file doesn't
   exist yet. It also locates the `tproxy-server` binary itself (used later to validate
   candidate profile files before they're written).
3. **Proxy hostname** — the domain tproxy-server already serves, pre-filled from
   `config.json` when detectable. Must be a lowercase DNS-style hostname.
4. **Telegram bot token** (from @BotFather) and **admin Telegram ID** — both loosely
   validated (token shape, numeric ID), with an "use it anyway" override if the format check
   is a false positive.
5. **Admin panel login** (default `admin`) and **password**, entered twice with echo off.
   Only the password's bcrypt hash ever reaches disk.

From there it runs unattended:

- Generates a random 20-character `PANEL_PATH_TOKEN` (the panel's secret URL segment) and a
  random 32-byte `SESSION_SECRET`.
- Backs up `/etc/caddy/Caddyfile` with a timestamp, then inserts (or, on a re-run, regenerates)
  one marked `handle /<TOKEN>/* { reverse_proxy 127.0.0.1:9000 }` block ahead of the existing
  catch-all — validated with `caddy validate` against a temp copy first. If validation fails,
  the live Caddyfile is left untouched; nothing is rolled back because nothing was written.
- Creates a dedicated unprivileged `tgproxy-panel` system user/group and the data/backup
  directories.
- Downloads the `tgproxy-panel-linux-amd64` binary from the latest GitHub release, or builds it
  from source with a local Go toolchain if the download fails.
- Hashes the admin password (via the binary's own `-hash-password` flag) and writes `.env`.
- Installs `deploy/apply-profiles.sh` at its fixed path (`/opt/tgproxy-panel/bin/`), a narrow
  `sudoers.d` rule permitting only that script, and the `tgproxy-panel.service` systemd unit —
  templated to the chosen install directory and validated (`visudo -c`) before being written.
- Enables and starts the service, waiting up to 10 seconds for it to report `active` (printing
  status/logs and exiting non-zero if it doesn't).

On success it prints the panel URL and a reminder of the admin login:

```
==================================================================
tgproxy-panel installed.

Panel URL:   https://<your-hostname>/<20-char-token>/
Admin login: <your login> (password: as entered above, not shown again)

Check:  systemctl --no-pager --full status tgproxy-panel
Logs:   journalctl -u tgproxy-panel -f
==================================================================
```

## What install.sh sets up

- **Caddy** — one additional route on the existing site block: the secret path proxies to the
  panel on `127.0.0.1:9000`, everything else keeps going to tproxy-server's relay on
  `127.0.0.1:8080` exactly as before.
- **A dedicated `tgproxy-panel` system user** — the panel process itself runs unprivileged,
  with no ability to write `/etc/tproxy-server/` or restart services directly.
- **A narrow `sudoers.d` rule** — grants `tgproxy-panel` passwordless `sudo` to run exactly one
  script, `/opt/tgproxy-panel/bin/apply-profiles.sh`, and only with an argument under
  `<INSTALL_DIR>/backup/candidates/*`. That script is the only thing on the host allowed to
  write `profiles.json` and restart `tproxy-server`; it re-validates every candidate itself
  (via tproxy-server's own `-check`) rather than trusting the unprivileged caller.
- **A systemd service** (`tgproxy-panel.service`) running the panel binary, restarting on
  failure, with `ReadWritePaths` scoped to just its own `data/` and `backup/` directories.

See `deploy/apply-profiles.sh` and `deploy/tgproxy-panel.service` for the full detail — both
carry extensive header comments explaining the privilege-separation design, including why the
service unit deliberately does **not** set `NoNewPrivileges=true` (that would silently break
every `sudo apply-profiles.sh` call).

## Post-install verification

```bash
systemctl --no-pager --full status tgproxy-panel
journalctl -u tgproxy-panel -f
```

Then open the printed panel URL (`https://<hostname>/<token>/`) in a browser and log in with
the admin credentials you set during install. A blank or default Caddy page instead of the
login screen usually means the Caddyfile patch didn't apply — check `systemctl status caddy`
and `journalctl -u caddy` next.

tproxy-server itself is unaffected by this install (no relay restart happens until you issue or
revoke a user), but if you want to double check it's still healthy:

```bash
curl --fail http://127.0.0.1:8081/healthz
curl --fail http://127.0.0.1:8081/readyz
```

## Usage

1. The admin opens the panel URL and logs in.
2. A user starts the bot and sends `/start`, then taps the **"🔑 Получить прокси"** inline
   button.
   - If they already have an active profile, the bot immediately re-sends their existing
     link and QR code.
   - If auto-issue is on, a profile is created and issued right away.
   - If auto-issue is off, a pending request is created; the user is told to wait, and the
     admin gets a Telegram notification with **"✅ Одобрить"** / **"❌ Отклонить"** buttons
     (the same request also shows up in the panel).
3. The admin approves or denies from either the bot or the panel — both call the same
   issue/revoke logic, so behavior is identical either way.
4. The panel's user list shows every user's status (pending/active/revoked/denied) with
   revoke/approve/deny actions, and each user's detail page shows their secret, ready-made
   `https://t.me/webproxy?server=...&secret=...` link, and QR code.
5. Auto-issue can be toggled at any time from the panel's settings.

## Updating

There is no dedicated in-place update path yet. Two options, depending on what you're
changing:

- **Binary-only update** (recommended for a routine version bump): stop the service, replace
  the binary at `<INSTALL_DIR>/tgproxy-panel` with a new release asset or a fresh
  `GOOS=linux GOARCH=amd64 go build`, then start it again:

  ```bash
  sudo systemctl stop tgproxy-panel
  sudo install -m 0755 -o root -g root tgproxy-panel-linux-amd64 <INSTALL_DIR>/tgproxy-panel
  sudo systemctl start tgproxy-panel
  ```

  The systemd unit's `EnvironmentFile` and `WorkingDirectory` stay pointed at
  `<INSTALL_DIR>/.env` and `<INSTALL_DIR>/data`/`backup`, so this preserves your existing
  configuration, database, and backups untouched.

- **Re-running `deploy/install.sh`** is *not* an in-place update: it unconditionally generates
  a fresh `PANEL_PATH_TOKEN` (changing the panel URL) and a fresh `SESSION_SECRET`, and
  re-prompts for admin login/password, overwriting the existing ones. Only do this if you
  actually want to rotate those.

## Security notes

- The panel is reachable only through Caddy, at your hostname plus a random 20-character path
  generated once at install time. That path isn't indexed or linked anywhere, but it is not a
  substitute for the login screen behind it.
- Admin credentials live only in `<INSTALL_DIR>/.env` (mode `0600`, owned by the `tgproxy-panel`
  user) as a bcrypt hash — the plaintext password is never written to disk and never appears in
  the repository.
- Session cookies are HMAC-signed with a random `SESSION_SECRET`; login attempts are
  rate-limited.
- Privileged operations are scoped to one root-owned script (`apply-profiles.sh`) invoked
  through one narrow `sudoers` rule — the panel process itself has no other path to root.
- `profiles.json`'s ownership and permissions (`root:tproxy`, `0400`) are re-asserted on every
  write, not just inherited from a temp file.
- Every `profiles.json` change is backed up with a UTC timestamp before being applied; the
  newest 100 backups are kept.
