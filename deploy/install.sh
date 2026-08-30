#!/usr/bin/env bash
#
# install.sh — interactive installer for tgproxy-panel (plan.md §8).
#
# Run as root, from inside a clone of this repository:
#   git clone https://github.com/barakov-dot/tgproxy-panel-q.git
#   cd tgproxy-panel-q
#   sudo ./deploy/install.sh
#
# Precondition: tproxy-server is already installed and working on this host
# (Caddy, MTProxy, /etc/tproxy-server/{config,profiles}.json all in place —
# see telegramdesktop/tproxy-server). This script never touches
# tproxy-server's relay logic; its write surface is: profiles.json (via
# deploy/apply-profiles.sh, installed here but invoked later by the panel
# process itself), one managed block in /etc/caddy/Caddyfile, plus its own
# systemd unit/sudoers rule/.env/data/backup directories.
#
# --- Path resolution (read this before changing any path logic below) ---
#
# INSTALL_DIR (prompted, default /opt/tgproxy-panel) is where the PANEL's
# own files live: the binary, .env, data/, backup/. It can be anything the
# admin chooses.
#
# By contrast, deploy/apply-profiles.sh and deploy/sudoers.tgproxy-panel
# (committed as static artifacts in a previous stage) hardcode
# /opt/tgproxy-panel/bin/apply-profiles.sh as the one script sudo may run.
# That path is NOT derived from INSTALL_DIR — it is always fixed at
# /opt/tgproxy-panel/bin/apply-profiles.sh, and likewise its override env
# file always lives at the fixed /opt/tgproxy-panel/apply-profiles.env
# (apply-profiles.sh's own ENV_FILE constant). Both are root-owned,
# privileged-script paths that exist independently of wherever the
# unprivileged panel process itself is installed.
#
# The one place INSTALL_DIR actually reaches those fixed-path artifacts is
# data: apply-profiles.env's BACKUP_DIR value and the sudoers rule's
# argument-glob both need to point at INSTALL_DIR/backup (that's where
# internal/applier actually stages candidate files), even though the
# script/rule/env-file *paths themselves* stay fixed. So:
#
#   Always fixed, independent of INSTALL_DIR:
#     /opt/tgproxy-panel/bin/apply-profiles.sh   (the script itself)
#     /opt/tgproxy-panel/apply-profiles.env      (its override env file)
#     /etc/sudoers.d/tgproxy-panel               (grants running that fixed script)
#     /etc/systemd/system/tgproxy-panel.service  (unit file location itself)
#
#   Always templated from INSTALL_DIR, every run, one code path (never
#   conditionally skipped "because it's the default" — this keeps default
#   and custom installs byte-identical in shape and equally well-tested):
#     apply-profiles.env's BACKUP_DIR=INSTALL_DIR/backup and the other
#       TPROXY_* values (written unconditionally, not only when they
#       diverge from apply-profiles.sh's own hardcoded defaults)
#     the sudoers rule's argument pattern: INSTALL_DIR/backup/candidates/*
#       (the command path granted, /opt/tgproxy-panel/bin/apply-profiles.sh,
#       stays fixed as above — only the argument glob is templated)
#     tgproxy-panel.service's WorkingDirectory=/EnvironmentFile=/ExecStart=/
#       ReadWritePaths= (all four re-anchored at INSTALL_DIR)
#
# At the default INSTALL_DIR=/opt/tgproxy-panel this produces output
# byte-identical (modulo the templated lines' own values, which then equal
# the committed originals) to the two static artifacts committed earlier.
#
# --- Caddyfile idempotency design ---
#
# tproxy-server's stock Caddyfile site block is NOT wrapped in `handle`
# blocks — see CLAUDE.md's "Verified facts". This script must, once,
# structurally rewrap the existing bare `reverse_proxy 127.0.0.1:8080 {...}`
# in a catch-all `handle { }`, and insert our own secret-path `handle
# /<TOKEN>/* { reverse_proxy 127.0.0.1:9000 }` block before it. That
# one-time structural rewrap must never run twice (it would double-wrap).
#
# Our own inserted block, however, legitimately needs to change on a
# re-run (a fresh PANEL_PATH_TOKEN is generated every install.sh run). So
# it alone is bounded by "# BEGIN/END tgproxy-panel managed block" marker
# comments, and a re-run that finds those markers does a clean
# find-the-markers-and-replace-between-them instead of re-doing the
# structural rewrap. Three cases, detected up front:
#   "fresh"  — markers absent, but the expected pristine bare
#              `reverse_proxy 127.0.0.1:8080 {` line is found: do the
#              one-time structural rewrap AND insert the marked block.
#   "update" — markers already present (an earlier install.sh run already
#              did the structural rewrap): replace only the content
#              between the markers, leave everything else untouched.
#   neither  — the file doesn't match either shape (hand-edited, or some
#              other unexpected structure): abort with a clear message
#              rather than risk corrupting it.
#
# The patched result is validated with `caddy validate` against a TEMP
# COPY before ever touching the live Caddyfile — so on validation failure
# there is nothing to roll back, the live file was simply never written.
# A timestamped backup of the pre-patch file is still taken first (plan.md
# step 7) for the admin's own reference/history.

set -euo pipefail
umask 077

PROG="install.sh"
die() { echo "${PROG}: $*" >&2; exit 1; }
info() { echo "${PROG}: $*"; }

# Every mktemp'd file created below is registered here and removed on exit,
# success or failure alike.
TMP_FILES=()
cleanup() {
    local f
    for f in "${TMP_FILES[@]}"; do
        [ -n "$f" ] && rm -f -- "$f"
    done
}
trap cleanup EXIT

new_tmp() {
    local t
    t="$(mktemp)"
    TMP_FILES+=("$t")
    printf '%s' "$t"
}

# prompt_default VAR "question" "default" — reads a line into VAR, falling
# back to "default" (shown in brackets, or omitted if empty) when blank.
prompt_default() {
    local __var="$1" __question="$2" __default="$3" __answer=""
    if [ -n "$__default" ]; then
        read -r -p "${__question} [${__default}]: " __answer || true
    else
        read -r -p "${__question}: " __answer || true
    fi
    [ -z "$__answer" ] && __answer="$__default"
    printf -v "$__var" '%s' "$__answer"
}

# json_field FILE KEY — best-effort extraction of a flat string/number
# value for KEY anywhere in a small JSON file (config.json/profiles.json
# are both small and shallow, so "anywhere" is precise enough in practice).
# Tries python3, then jq, then a defensive grep -oP fallback (all three are
# plausible-but-not-guaranteed on a minimal Debian/Ubuntu box); prints
# nothing and returns success either way, since every caller treats this as
# a convenience default, never a hard requirement.
# find_mtproxy_port prints the loopback port mtproto-proxy listens on, if any.
find_mtproxy_port() {
    local port=""
    if command -v ss >/dev/null 2>&1; then
        port="$(ss -lntp 2>/dev/null | awk '
            /127\.0\.0\.1:[0-9]+/ && /mtproto-proxy/ {
                if (match($0, /127\.0\.0\.1:([0-9]+)/, a)) {
                    print a[1]
                    exit
                }
            }')"
        if [ -n "$port" ]; then
            printf '%s' "$port"
            return 0
        fi
        if ss -lnt 2>/dev/null | grep -Eq ':2398\b'; then
            printf '2398'
            return 0
        fi
    fi
    return 1
}

json_field() {
    local file="$1" key="$2"
    [ -f "$file" ] || return 0
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$file" "$key" <<'PYEOF' 2>/dev/null || true
import json, sys

try:
    with open(sys.argv[1]) as fh:
        data = json.load(fh)
except Exception:
    sys.exit(0)

key = sys.argv[2]


def find(obj):
    if isinstance(obj, dict):
        if key in obj and isinstance(obj[key], (str, int, float)):
            return str(obj[key])
        for v in obj.values():
            r = find(v)
            if r is not None:
                return r
    elif isinstance(obj, list):
        for v in obj:
            r = find(v)
            if r is not None:
                return r
    return None


result = find(data)
if result is not None:
    print(result)
PYEOF
    elif command -v jq >/dev/null 2>&1; then
        jq -r --arg k "$key" '[.. | objects | .[$k]? // empty] | first // empty' "$file" 2>/dev/null || true
    else
        grep -oP "(?<=\"${key}\"[[:space:]]{0,5}:[[:space:]]{0,5}\")[^\"]*" "$file" 2>/dev/null | head -n1 || true
    fi
}

# --- 1. Must run as root ---

if [ "$(id -u)" -ne 0 ]; then
    die "run this installer as root (e.g. via sudo ./deploy/install.sh)"
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# --- Preconditions: caddy must already be installed (tproxy-server's job) ---

caddy_bin=""
if command -v caddy >/dev/null 2>&1; then
    caddy_bin="$(command -v caddy)"
elif [ -x /usr/local/bin/caddy ]; then
    caddy_bin="/usr/local/bin/caddy"
else
    die "caddy binary not found (checked PATH and /usr/local/bin/caddy) — is tproxy-server (which installs Caddy) already installed on this host?"
fi

caddyfile_path="/etc/caddy/Caddyfile"
[ -f "$caddyfile_path" ] || die "Caddyfile not found at $caddyfile_path — is tproxy-server already installed?"

echo "tgproxy-panel installer"
echo "========================"
echo

# --- 2. Install directory (plan.md §8 step 1) ---

prompt_default install_dir "Install directory" "/opt/tgproxy-panel"
case "$install_dir" in
    /*) ;;
    *) die "install directory must be an absolute path, got: $install_dir" ;;
esac
install_dir="${install_dir%/}"
[ -n "$install_dir" ] || die "install directory must not be '/'"

# --- 3. tproxy-server's profiles.json / config.json paths (plan.md step 2) ---

default_profiles_path="/etc/tproxy-server/profiles.json"
default_config_path="/etc/tproxy-server/config.json"

service_found=0
if systemctl cat tproxy-server >/dev/null 2>&1; then
    service_found=1
fi

if [ "$service_found" -eq 1 ] && [ -e "$default_profiles_path" ] && [ -e "$default_config_path" ]; then
    profiles_path="$default_profiles_path"
    config_path="$default_config_path"
    info "auto-detected tproxy-server profiles.json and config.json at their default paths"
else
    if [ "$service_found" -eq 0 ]; then
        echo "Warning: no 'tproxy-server' systemd service found on this host." >&2
    fi
    prompt_default profiles_path "Path to tproxy-server's profiles.json" "$default_profiles_path"
    prompt_default config_path "Path to tproxy-server's config.json" "$default_config_path"
fi
[ -f "$profiles_path" ] || echo "Warning: $profiles_path does not exist yet." >&2
[ -f "$config_path" ] || echo "Warning: $config_path does not exist yet." >&2

tproxy_server_bin=""
if command -v tproxy-server >/dev/null 2>&1; then
    tproxy_server_bin="$(command -v tproxy-server)"
elif [ -x /usr/local/bin/tproxy-server ]; then
    tproxy_server_bin="/usr/local/bin/tproxy-server"
else
    echo "Warning: tproxy-server binary not found on PATH or at /usr/local/bin/tproxy-server." >&2
    prompt_default tproxy_server_bin "Path to the tproxy-server binary" "/usr/local/bin/tproxy-server"
    if [ ! -x "$tproxy_server_bin" ]; then
        echo "Warning: $tproxy_server_bin is missing or not executable — every future apply's '-check' validation step will fail until this is fixed (see .env's TPROXY_SERVER_BIN)." >&2
    fi
fi

# --- 4. Proxy hostname (plan.md step 3) ---

detected_hostname="$(json_field "$config_path" "public_hostname")"
while :; do
    prompt_default tproxy_hostname "Proxy hostname (same domain tproxy-server already serves)" "$detected_hostname"
    # shellcheck disable=SC2154  # set indirectly by prompt_default's printf -v
    if [[ "$tproxy_hostname" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] && [[ "$tproxy_hostname" == *.* ]]; then
        break
    fi
    echo "That doesn't look like a valid lowercase DNS hostname; try again." >&2
    detected_hostname=""
done

# --- 5. Telegram bot token + admin Telegram ID (plan.md step 4) ---

bot_token=""
while :; do
    prompt_default bot_token "Telegram bot token (from @BotFather)" "$bot_token"
    if [[ "$bot_token" =~ ^[0-9]{6,}:[A-Za-z0-9_-]{20,}$ ]]; then
        break
    fi
    echo "Warning: '$bot_token' does not look like a bot token (expected NNNNNNNN:AAAA...)." >&2
    read -r -p "Use it anyway? [y/N]: " confirm || true
    [[ "$confirm" =~ ^[Yy]$ ]] && break
done

admin_telegram_id=""
while :; do
    prompt_default admin_telegram_id "Admin Telegram numeric ID (receives request notifications)" "$admin_telegram_id"
    [[ "$admin_telegram_id" =~ ^[0-9]+$ ]] && break
    echo "Warning: '$admin_telegram_id' is not a plain number." >&2
    read -r -p "Use it anyway? [y/N]: " confirm || true
    [[ "$confirm" =~ ^[Yy]$ ]] && break
done

# --- 6. Admin login/password for the panel (plan.md step 5) ---
#
# Only the plaintext is collected here; hashing needs the actual panel
# binary (tgproxy-panel -hash-password, see cmd/tgproxy-panel/main.go),
# which does not exist yet at this point in the script. The hash is
# computed later (step 14, after the binary is downloaded/built), and
# admin_password is unset immediately afterward.

prompt_default admin_login "Admin panel login" "admin"

admin_password=""
while :; do
    read -r -s -p "Admin panel password: " admin_password_1
    echo
    read -r -s -p "Confirm admin panel password: " admin_password_2
    echo
    if [ -z "$admin_password_1" ]; then
        echo "Password must not be empty." >&2
        continue
    fi
    if [ "$admin_password_1" != "$admin_password_2" ]; then
        echo "Passwords did not match, try again." >&2
        continue
    fi
    admin_password="$admin_password_1"
    break
done
unset admin_password_1 admin_password_2

# --- 7. Random 20-char path token (plan.md step 6; recipe matches .env.example) ---
#
# Run in a subshell with pipefail off: `tr` reads /dev/urandom (an infinite
# source) and keeps writing until `head -c20` has read its fill and closes
# the pipe, which sends `tr` a SIGPIPE (exit 141). Under this script's own
# `set -euo pipefail`, that non-zero status would otherwise abort the whole
# installer right here with no error message the moment `head` is satisfied
# — `head` itself still exits 0 with the correct 20 bytes, so the pipeline's
# actual result is fine; only pipefail's bookkeeping treats it as a failure.
panel_path_token="$(set +o pipefail; tr -dc 'a-zA-Z0-9' </dev/urandom | head -c20)"

if command -v openssl >/dev/null 2>&1; then
    session_secret="$(openssl rand -hex 32)"
else
    session_secret="$(head -c32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
fi

# --- Directories needed before the Caddyfile backup step ---

install -d -m 0755 -o root -g root "$install_dir"
install -d -m 0700 -o root -g root "$install_dir/backup"

# --- 8. Backup /etc/caddy/Caddyfile (plan.md step 7) ---

ts="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
caddyfile_backup="$install_dir/backup/Caddyfile.${ts}.bak"
cp -p -- "$caddyfile_path" "$caddyfile_backup"
info "backed up current Caddyfile to $caddyfile_backup"

# --- 9. Patch the Caddyfile (plan.md step 8) ---

marker_begin="# BEGIN tgproxy-panel managed block"
marker_end="# END tgproxy-panel managed block"

patch_mode=""
if grep -qF "$marker_begin" "$caddyfile_path"; then
    patch_mode="update"
elif grep -qE '^[[:space:]]*reverse_proxy 127\.0\.0\.1:8080 \{[[:space:]]*$' "$caddyfile_path" \
    && grep -qF 'handle_errors {' "$caddyfile_path"; then
    patch_mode="fresh"
else
    die "Caddyfile at $caddyfile_path does not match the expected tproxy-server structure and has no tgproxy-panel managed block already. Refusing to patch automatically to avoid corrupting a hand-edited file. Add manually: a 'handle /${panel_path_token}/* { reverse_proxy 127.0.0.1:9000 }' block before the existing reverse_proxy, with that reverse_proxy then wrapped in a bare 'handle { }' — see CLAUDE.md's Verified facts section for the exact target shape."
fi

patched_tmp="$(new_tmp)"

case "$patch_mode" in
    fresh)
        info "Caddyfile: pristine tproxy-server structure detected, performing one-time structural patch"
        awk -v token="$panel_path_token" '
            BEGIN { in_block = 0; depth = 0 }
            in_block == 0 && $0 ~ /^[[:space:]]*reverse_proxy 127\.0\.0\.1:8080 \{[[:space:]]*$/ {
                print "\t# BEGIN tgproxy-panel managed block (regenerated by deploy/install.sh -- do not edit by hand)"
                print "\thandle /" token "/* {"
                print "\t\treverse_proxy 127.0.0.1:9000"
                print "\t}"
                print "\t# END tgproxy-panel managed block"
                print "\thandle {"
                print "\t" $0
                in_block = 1
                depth = 1
                next
            }
            in_block == 1 {
                nopen = gsub(/\{/, "{")
                nclose = gsub(/\}/, "}")
                depth += nopen - nclose
                if (depth == 0) {
                    print "\t" $0
                    print "\t}"
                    in_block = 0
                } else {
                    print "\t" $0
                }
                next
            }
            { print }
        ' "$caddyfile_path" >"$patched_tmp"
        ;;
    update)
        info "Caddyfile: existing tgproxy-panel managed block found, regenerating it in place (structure otherwise untouched)"
        awk -v token="$panel_path_token" -v bm="$marker_begin" -v em="$marker_end" '
            index($0, bm) > 0 {
                print "\t" bm " (regenerated by deploy/install.sh -- do not edit by hand)"
                print "\thandle /" token "/* {"
                print "\t\treverse_proxy 127.0.0.1:9000"
                print "\t}"
                print "\t" em
                skip = 1
                next
            }
            index($0, em) > 0 {
                skip = 0
                next
            }
            skip == 1 { next }
            { print }
        ' "$caddyfile_path" >"$patched_tmp"
        ;;
esac

# --- 10. Validate before ever touching the live file ---
#
# Values feeding the Caddyfile's {$VAR} placeholders (global options block
# + site header). Read from the systemd drop-in tproxy-server's own
# installer wrote, when present, so this validate call sees an environment
# equivalent to caddy.service's real one. This only affects THIS one-off
# syntax check — caddy.service's own drop-in file is never modified here.
caddy_dropin="/etc/systemd/system/caddy.service.d/tproxy.conf"
validate_site_root="/srv/tproxy-site"
validate_acme_email="admin@invalid.example"
if [ -f "$caddy_dropin" ]; then
    v="$(grep -oP '(?<=^Environment=TPROXY_SITE_ROOT=).*' "$caddy_dropin" 2>/dev/null || true)"
    [ -n "$v" ] && validate_site_root="$v"
    v="$(grep -oP '(?<=^Environment=ACME_EMAIL=).*' "$caddy_dropin" 2>/dev/null || true)"
    [ -n "$v" ] && validate_acme_email="$v"
fi

if ! TPROXY_HOSTNAME="$tproxy_hostname" TPROXY_SITE_ROOT="$validate_site_root" ACME_EMAIL="$validate_acme_email" \
    "$caddy_bin" validate --config "$patched_tmp" --adapter caddyfile; then
    die "caddy validate failed against the patched Caddyfile. The live file at $caddyfile_path was NOT modified (a pre-patch copy is saved at $caddyfile_backup, though nothing needs restoring since nothing was written)."
fi

# A restart, not a reload: tproxy-server's own deploy/caddy.service unit
# (installed by tproxy-server's installer, not this project) defines no
# ExecReload=, so `systemctl reload caddy` fails outright with "Job type
# reload is not applicable for unit caddy.service" — confirmed against a
# real install. The Caddyfile's own global options block also sets
# `admin off`, which rules out `caddy reload`'s CLI (it talks to the local
# admin API to push a live config, and that API is deliberately disabled)
# as an alternative. A restart is the only supported way to make this
# specific unit pick up a changed Caddyfile; it's brief (sub-second,
# in-memory TLS state is unaffected since certificates are cached on disk)
# and happens once here at install time, not on every issue/revoke like
# tproxy-server's own unavoidable restart.
cp -- "$patched_tmp" "$caddyfile_path"
if ! systemctl restart caddy; then
    echo "install.sh: systemctl restart caddy failed, restoring the pre-patch Caddyfile from $caddyfile_backup" >&2
    cp -- "$caddyfile_backup" "$caddyfile_path"
    systemctl restart caddy || true
    die "systemctl restart caddy failed after installing the patched Caddyfile; restored the previous Caddyfile from $caddyfile_backup and re-attempted a restart with it. Check 'systemctl status caddy' and 'journalctl -u caddy' before re-running this installer."
fi
info "Caddyfile patched, validated, and Caddy restarted"

# --- 11. tgproxy-panel system user/group (idempotent) ---

if ! getent group tgproxy-panel >/dev/null 2>&1; then
    groupadd --system tgproxy-panel
fi
if ! id tgproxy-panel >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin -g tgproxy-panel tgproxy-panel
fi

# internal/applier shells out to `tproxy-server -config ... -check` as this
# unprivileged process itself — a fail-fast validation of the candidate
# profiles.json *before* ever invoking sudo apply-profiles.sh. That needs
# read access to config.json, which tproxy-server's own installer leaves at
# root:tproxy 0640. Add tgproxy-panel to config.json's actual owning group
# (normally "tproxy") so that read succeeds. This grants nothing beyond
# read access to a secrets-free settings file (public_hostname/limits/
# timeouts) — it does NOT grant any access to profiles.json (0400, no
# group bits at all), which the panel process still cannot and must not
# read directly (see CLAUDE.md's "Verified facts" / internal/applier's
# declarative, DB-is-source-of-truth design). Confirmed against a real
# install: without this, every issue/approve fails with "permission
# denied" opening config.json, even though apply-profiles.sh's own
# root-context validation would have succeeded fine.
if [ -e "$config_path" ]; then
    config_group="$(stat -c '%G' "$config_path" 2>/dev/null || true)"
    if [ -n "$config_group" ] && getent group "$config_group" >/dev/null 2>&1; then
        usermod -aG "$config_group" tgproxy-panel
    else
        echo "Warning: could not determine/verify the group owning $config_path; tgproxy-panel may not be able to read it, which would make the panel's own pre-flight profile validation fail with a permission error (deploy/apply-profiles.sh's own root-context validation is unaffected)." >&2
    fi
fi

install -d -m 0700 -o tgproxy-panel -g tgproxy-panel "$install_dir/data"
chown tgproxy-panel:tgproxy-panel "$install_dir/backup"
chmod 0700 "$install_dir/backup"

# --- 12. Acquire the tgproxy-panel binary (plan.md §12) ---

binary_path="$install_dir/tgproxy-panel"
release_url="https://github.com/barakov-dot/tgproxy-panel-q/releases/latest/download/tgproxy-panel-linux-amd64"
binary_tmp="$(new_tmp)"

info "attempting to download release binary from $release_url"
download_ok=1
curl -fL --show-error -o "$binary_tmp" "$release_url" || download_ok=0

if [ "$download_ok" -eq 1 ] && [ -s "$binary_tmp" ]; then
    info "downloaded release binary"
    install -m 0755 -o root -g root "$binary_tmp" "$binary_path"
elif command -v go >/dev/null 2>&1; then
    info "release download unavailable, building from source with the local Go toolchain"
    if ! (cd "$repo_root" && go build -trimpath -o "$binary_tmp" ./cmd/tgproxy-panel); then
        die "go build failed (see output above)"
    fi
    install -m 0755 -o root -g root "$binary_tmp" "$binary_path"
else
    die "could not obtain the tgproxy-panel binary: release download failed and no local Go toolchain is available. Install Go, or wait for a release to be published, then re-run this installer."
fi

# --- 13. Hash the admin password (needs the binary from step 12) ---

admin_password_hash="$(printf '%s\n' "$admin_password" | "$binary_path" -hash-password)"
unset admin_password
# shellcheck disable=SC2016  # deliberately literal: matching a bcrypt "$2" prefix, not expanding $2
case "$admin_password_hash" in
    '$2'*) ;;
    *) die "failed to generate a bcrypt password hash via '$binary_path -hash-password'" ;;
esac

# --- 14. Install apply-profiles.sh at its fixed path (see header comment) ---

install -d -m 0755 -o root -g root /opt/tgproxy-panel/bin
install -m 0755 -o root -g root "$repo_root/deploy/apply-profiles.sh" /opt/tgproxy-panel/bin/apply-profiles.sh
install -m 0755 -o root -g root "$repo_root/deploy/mtproxy-exec.sh" /opt/tgproxy-panel/bin/mtproxy-exec.sh

# MTProxy multi-secret: wrapper + systemd drop-in so every profile secret gets
# its own `-S` on the single official mtproto-proxy (see apply-profiles.sh 3d).
mtproxy_stats_port="8888"
mtproxy_listen_port="2398"
if mtproxy_port="$(find_mtproxy_port 2>/dev/null)"; then
    mtproxy_listen_port="$mtproxy_port"
fi
if [ -f /etc/systemd/system/mtproxy.service ]; then
    parsed_stats="$(grep -Eo '\-p[[:space:]]+[0-9]+' /etc/systemd/system/mtproxy.service 2>/dev/null | awk '{print $2}' | head -n1 || true)"
    parsed_listen="$(grep -Eo '\-H[[:space:]]+[0-9]+' /etc/systemd/system/mtproxy.service 2>/dev/null | awk '{print $2}' | head -n1 || true)"
    [ -n "$parsed_stats" ] && mtproxy_stats_port="$parsed_stats"
    [ -n "$parsed_listen" ] && mtproxy_listen_port="$parsed_listen"
fi
if systemctl list-unit-files --no-legend mtproxy.service 2>/dev/null | grep -q '^'; then
    install -d -m 0755 /etc/systemd/system/mtproxy.service.d
    cat > /etc/systemd/system/mtproxy.service.d/tgproxy-panel.conf <<EOF
[Service]
Environment=MTPROXY_STATS_PORT=$mtproxy_stats_port
Environment=MTPROXY_LISTEN_PORT=$mtproxy_listen_port
ExecStart=
ExecStart=/opt/tgproxy-panel/bin/mtproxy-exec.sh
EOF
    install -d -m 0750 -o root -g mtproxy /etc/mtproxy
    if [ -f "$profiles_path" ] && command -v python3 >/dev/null 2>&1; then
        python3 - "$profiles_path" /etc/mtproxy/mtproxy.env <<'PY' || true
import json, os, re, sys

profiles_path, env_path = sys.argv[1:3]
secret_re = re.compile(r"^(?:dd)?[0-9a-f]{32}$")
seen = []
for profile in json.load(open(profiles_path, encoding="utf-8")).get("profiles", []):
    raw = str(profile.get("secret", "")).strip().lower()
    if raw.startswith("dd") and len(raw) == 34:
        raw = raw[2:]
    if secret_re.fullmatch(raw) and raw not in seen:
        seen.append(raw)
if not seen:
    sys.exit(0)

lines = []
if os.path.isfile(env_path):
    with open(env_path, encoding="utf-8") as f:
        for line in f:
            stripped = line.strip()
            if stripped and not stripped.startswith("#") and "=" in stripped:
                key = stripped.split("=", 1)[0].strip()
                if key in ("MTPROXY_SECRET", "MTPROXY_SECRETS"):
                    continue
            lines.append(line.rstrip("\n"))

while lines and not lines[-1].strip():
    lines.pop()

lines.append(f"MTPROXY_SECRET={seen[0]}")
lines.append(f"MTPROXY_SECRETS={' '.join(seen)}")

with open(env_path, "w", encoding="utf-8") as f:
    f.write("\n".join(lines) + "\n")
PY
        [ -f /etc/mtproxy/mtproxy.env ] && \
            chown root:mtproxy /etc/mtproxy/mtproxy.env && \
            chmod 0440 /etc/mtproxy/mtproxy.env
    fi
    systemctl daemon-reload
    systemctl restart mtproxy.service 2>/dev/null || true
    info "configured MTProxy multi-secret wrapper (listen :${mtproxy_listen_port})"
fi

apply_profiles_env_tmp="$(new_tmp)"
cat >"$apply_profiles_env_tmp" <<EOF
TPROXY_PROFILES_PATH=$profiles_path
TPROXY_CONFIG_PATH=$config_path
TPROXY_SERVICE_NAME=tproxy-server
TPROXY_SERVER_BIN=$tproxy_server_bin
BACKUP_DIR=$install_dir/backup
BACKUP_KEEP=100
MTPROXY_SERVICE_NAME=mtproxy
MTPROXY_ENV_FILE=/etc/mtproxy/mtproxy.env
EOF
install -m 0600 -o root -g root "$apply_profiles_env_tmp" /opt/tgproxy-panel/apply-profiles.env

# --- 15. Sudoers rule, templated with the real install dir, then validated ---

sudoers_line="tgproxy-panel ALL=(root) NOPASSWD: /opt/tgproxy-panel/bin/apply-profiles.sh ${install_dir}/backup/candidates/*"
sudoers_tmp="$(new_tmp)"
awk -v line="$sudoers_line" '
    /^tgproxy-panel ALL=/ { print line; next }
    { print }
' "$repo_root/deploy/sudoers.tgproxy-panel" >"$sudoers_tmp"

if ! visudo -c -f "$sudoers_tmp"; then
    die "generated sudoers rule failed 'visudo -c -f' validation; nothing was written to /etc/sudoers.d"
fi
install -m 0440 -o root -g root "$sudoers_tmp" /etc/sudoers.d/tgproxy-panel
info "installed sudoers rule at /etc/sudoers.d/tgproxy-panel"

# --- 16. Write .env ---

detected_admin_listen="$(json_field "$config_path" "admin_listen")"
tproxy_admin_url="http://${detected_admin_listen:-127.0.0.1:8081}"
detected_backend="$(json_field "$profiles_path" "backend")"
detected_carrier_mode="$(json_field "$profiles_path" "carrier_mode")"
tproxy_carrier_mode="${detected_carrier_mode:-https}"
if mtproxy_port="$(find_mtproxy_port 2>/dev/null)"; then
    tproxy_backend="127.0.0.1:${mtproxy_port}"
    info "detected MTProxy listening on loopback :${mtproxy_port}"
elif [ -n "$detected_backend" ]; then
    tproxy_backend="$detected_backend"
else
    tproxy_backend="127.0.0.1:2398"
fi

env_tmp="$(new_tmp)"
# shellcheck disable=SC2154  # admin_login/admin_password_hash set indirectly (prompt_default / step 13)
cat >"$env_tmp" <<EOF
PANEL_PORT=9000
PANEL_PATH_TOKEN=$panel_path_token

ADMIN_LOGIN=$admin_login
ADMIN_PASSWORD_HASH=$admin_password_hash
SESSION_SECRET=$session_secret

BOT_TOKEN=$bot_token
ADMIN_TELEGRAM_ID=$admin_telegram_id
AUTO_ISSUE=false

TPROXY_HOSTNAME=$tproxy_hostname
TPROXY_SERVICE_NAME=tproxy-server
TPROXY_PROFILES_PATH=$profiles_path
TPROXY_CONFIG_PATH=$config_path
TPROXY_ADMIN_URL=$tproxy_admin_url
CADDYFILE_PATH=$caddyfile_path
TPROXY_BACKEND=$tproxy_backend
TPROXY_CARRIER_MODE=$tproxy_carrier_mode

DB_PATH=$install_dir/data/panel.db
BACKUP_DIR=$install_dir/backup
BACKUP_KEEP=100

APPLY_PROFILES_SCRIPT=/opt/tgproxy-panel/bin/apply-profiles.sh
TPROXY_SERVER_BIN=$tproxy_server_bin

LOG_FORMAT=json
EOF
install -m 0600 -o tgproxy-panel -g tgproxy-panel "$env_tmp" "$install_dir/.env"

# --- 17. systemd unit, templated with the real install dir ---
#
# ReadWritePaths must also include profiles.json's own directory (normally
# /etc/tproxy-server): ProtectSystem=strict makes the whole filesystem
# read-only at the mount/namespace level for every process in this unit's
# tree, INCLUDING a root-escalated `sudo apply-profiles.sh` child — sudo
# changes the process's UID/GID, not its mount namespace, so root still
# can't write through a read-only bind mount without this. DAC permissions
# (config_path's group-read grant from step 11 above, profiles.json's own
# 0400 root:tproxy) are unaffected either way; this only concerns the
# separate mount-level restriction. Confirmed against a real install:
# without it, apply-profiles.sh's own mktemp call fails with "Read-only
# file system" even though it's genuinely running as root by that point.
profiles_dir="$(dirname -- "$profiles_path")"

service_tmp="$(new_tmp)"
sed \
    -e "s|^WorkingDirectory=.*|WorkingDirectory=${install_dir}|" \
    -e "s|^EnvironmentFile=.*|EnvironmentFile=${install_dir}/.env|" \
    -e "s|^ExecStart=.*|ExecStart=${install_dir}/tgproxy-panel|" \
    -e "s|^ReadWritePaths=.*|ReadWritePaths=${install_dir}/data ${install_dir}/backup ${profiles_dir}|" \
    "$repo_root/deploy/tgproxy-panel.service" >"$service_tmp"
install -m 0644 -o root -g root "$service_tmp" /etc/systemd/system/tgproxy-panel.service
systemctl daemon-reload

# --- 18. Enable and start ---
#
# Explicit enable + restart, not `enable --now`: on a re-run against an
# already-active service, `enable --now` only calls the equivalent of
# `systemctl start`, which is a no-op on a unit that's already running — so
# a re-run meant to pick up a new binary, .env, or (see above) group
# membership would silently keep running the OLD process state. `restart`
# is safe unconditionally (starts it if stopped, restarts it if running).
systemctl enable tgproxy-panel
systemctl restart tgproxy-panel

active=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
    if systemctl is-active --quiet tgproxy-panel; then
        active=1
        break
    fi
    sleep 1
done

if [ "$active" -ne 1 ]; then
    echo "tgproxy-panel did not report 'active' within 10s. Recent status/logs:" >&2
    systemctl --no-pager --full status tgproxy-panel >&2 || true
    journalctl -u tgproxy-panel -n 50 --no-pager >&2 || true
    die "tgproxy-panel failed to start — see status/logs above"
fi
info "tgproxy-panel is active"

# --- 19. Final summary (plan.md step 12) ---

echo
echo "=================================================================="
echo "tgproxy-panel installed."
echo
echo "Panel URL:   https://${tproxy_hostname}/${panel_path_token}/"
echo "Admin login: ${admin_login} (password: as entered above, not shown again)"
echo
echo "Check:  systemctl --no-pager --full status tgproxy-panel"
echo "Logs:   journalctl -u tgproxy-panel -f"
echo "=================================================================="
