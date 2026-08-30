#!/usr/bin/env bash
#
# apply-profiles.sh — root-owned privileged executor for tgproxy-panel.
#
# Invoked as: sudo /opt/tgproxy-panel/bin/apply-profiles.sh <candidate-path>
#
# This is the ONLY thing on the target host allowed to write
# /etc/tproxy-server/profiles.json and restart tproxy-server. The panel
# process itself (internal/applier, see applier.go's apply()) runs
# unprivileged as the `tgproxy-panel` system user; it stages a candidate
# JSON file under $BACKUP_DIR/candidates/ and invokes this script via a
# narrow sudoers NOPASSWD rule (deploy/sudoers.tgproxy-panel) that only
# permits running this exact script with an argument under that directory.
#
# Because the caller is unprivileged and this script is the actual
# privileged boundary, it NEVER trusts that the candidate was already
# validated by the caller (defense in depth) — it re-validates via
# tproxy-server's own `-check` (step 3) regardless of what the caller
# claims to have already done.
#
# Flow (plan.md §7 "Модуль применения изменений"):
#   1. Validate invocation (exactly one arg, existing regular file, no
#      path-traversal tricks).
#   2. Cheap structural JSON pre-filter (best-effort, not authoritative).
#   3. Authoritative validation: `tproxy-server -config ... -profiles-file
#      <candidate> -check`.
#   3b. Merge candidate with the live profiles.json: keep every profile whose
#      name is NOT panel-managed (user_<telegram_id>), replace all panel-managed
#      entries with the candidate list. This preserves pre-existing profiles
#      such as "default" that tproxy-server shipped with.
#   4. Backup the current live profiles.json (skip if this is the first
#      ever run and none exists yet).
#   5. Rotate backups, keeping only the newest BACKUP_KEEP.
#   6. Atomically install the candidate as the new profiles.json
#      (temp file in the same directory + chown root:tproxy + chmod 0400 +
#      rename).
#   7. systemctl restart the tproxy-server service. If systemctl itself
#      rejects the restart request, best-effort restore the backup taken
#      in step 4 before exiting non-zero, so the live file is left as
#      close as possible to how it was found.
#   8. Print a one-line, secret-free summary on every exit path.
#
# This script deliberately does NOT poll /readyz — internal/applier
# (applier.go's apply()) does that itself after this script returns 0.
# Success here means "the new profiles.json is live and a restart was
# issued", not "the service is confirmed healthy".
#
# --- Configuration contract for deploy/install.sh ---
#
# This script has no access to the unprivileged panel process's .env (it
# runs as root via sudo, not as the panel process), so every path/setting
# below has a hardcoded default matching CLAUDE.md's verified upstream
# facts and .env.example's defaults. If a deployment overrides one of the
# corresponding TPROXY_*/BACKUP_* variables in its .env away from the
# default, install.sh MUST generate an env file at the exact path below so
# this script picks up the same values — the panel and this script must
# agree on where profiles.json lives, or applies will silently target the
# wrong file.
#
#   Env file path : /opt/tgproxy-panel/apply-profiles.env
#   Format        : one VAR=value per line, no quoting/expansion, e.g.:
#                     TPROXY_PROFILES_PATH=/etc/tproxy-server/profiles.json
#                     TPROXY_CONFIG_PATH=/etc/tproxy-server/config.json
#                     TPROXY_SERVICE_NAME=tproxy-server
#                     TPROXY_SERVER_BIN=/usr/local/bin/tproxy-server
#                     BACKUP_DIR=/opt/tgproxy-panel/backup
#                     BACKUP_KEEP=100
#   Variable names: must match exactly (same spelling as internal/config's
#                   env vars for the overlapping settings). Any subset may
#                   be present; unset ones keep their hardcoded default
#                   below. The file is sourced with a plain `.`, so it
#                   must contain only VAR=value assignments — no command
#                   substitution, no untrusted content.
#   Who writes it : deploy/install.sh, generated from the panel's actual
#                   .env at install time. This script only reads it.
#
# Known coupling: deploy/sudoers.tgproxy-panel hardcodes
# /opt/tgproxy-panel/backup/candidates/* as the only argument pattern it
# permits. If BACKUP_DIR is ever overridden away from
# /opt/tgproxy-panel/backup, install.sh must also regenerate the sudoers
# file to match, or sudo will refuse to run this script at all.

set -euo pipefail

# --- Defaults (CLAUDE.md verified facts / .env.example) ---
TPROXY_PROFILES_PATH="/etc/tproxy-server/profiles.json"
TPROXY_CONFIG_PATH="/etc/tproxy-server/config.json"
TPROXY_SERVICE_NAME="tproxy-server"
TPROXY_SERVER_BIN="/usr/local/bin/tproxy-server"
BACKUP_DIR="/opt/tgproxy-panel/backup"
BACKUP_KEEP=100

# install.sh-generated overrides, if present.
ENV_FILE="/opt/tgproxy-panel/apply-profiles.env"
if [ -f "$ENV_FILE" ]; then
    # shellcheck source=/dev/null
    . "$ENV_FILE"
fi

# profiles.json's required owner/mode on disk (CLAUDE.md verified facts:
# root:tproxy 0400). Not part of the install.sh override contract above —
# this is a fixed property of how tproxy-server itself was installed, not
# a per-deployment setting.
readonly PROFILES_OWNER="root:tproxy"
readonly PROFILES_MODE="0400"

readonly PROG="apply-profiles.sh"

# Temp files created along the way, cleaned up on every exit path.
tmp_install=""
tmp_restore=""
tmp_merged=""
# shellcheck disable=SC2329  # invoked indirectly via `trap ... EXIT` below
cleanup() {
    [ -n "$tmp_install" ] && rm -f "$tmp_install"
    [ -n "$tmp_restore" ] && rm -f "$tmp_restore"
    [ -n "$tmp_merged" ] && rm -f "$tmp_merged"
    # Without this, `set -e` turns the false result of a no-op `[ -n "" ] &&
    # ...` above (the common case: nothing left to clean up) into this
    # function's return status, which — because it runs via `trap ... EXIT`
    # — silently overwrites an already-decided `exit 0` with exit 1. Caught
    # by testing the real happy path end-to-end, not by code review alone.
    return 0
}
trap cleanup EXIT

die() {
    echo "${PROG}: $*" >&2
    exit 1
}

info() {
    echo "${PROG}: $*"
}

# --- 1. Validate invocation ---

if [ "$#" -ne 1 ]; then
    die "expected exactly one argument (candidate profiles.json path), got $#"
fi

candidate_raw="$1"

case "$candidate_raw" in
    *..*)
        die "candidate path must not contain '..': $candidate_raw"
        ;;
esac

if ! candidate="$(realpath -e -- "$candidate_raw" 2>/dev/null)"; then
    die "candidate path does not exist or cannot be resolved: $candidate_raw"
fi

if [ ! -f "$candidate" ]; then
    die "candidate path is not a regular file: $candidate"
fi

# Defense in depth: the sudoers rule (deploy/sudoers.tgproxy-panel) already
# restricts this script's argument to $BACKUP_DIR/candidates/*, but that
# depends on sudoers glob matching lining up with reality. Enforce the same
# constraint here too, so a sudoers misconfiguration doesn't turn this
# script into an arbitrary-file installer.
candidate_dir="${BACKUP_DIR%/}/candidates"
case "$candidate" in
    "$candidate_dir"/*) ;;
    *) die "candidate path must be under $candidate_dir, got: $candidate" ;;
esac

info "candidate accepted: $candidate ($(wc -c < "$candidate") bytes)"

# --- 2. Cheap structural JSON pre-filter (best-effort, not authoritative) ---
#
# The real gate is step 3 (tproxy-server -check), which is the relay's own
# parser and always runs regardless. This step is just a fast, cheap
# fail-fast so a garbled candidate doesn't even reach step 3. Neither
# python3 nor jq is guaranteed present on a minimal Debian/Ubuntu install,
# so this step degrades gracefully to a no-op rather than hard-failing on
# a missing interpreter.
if command -v python3 >/dev/null 2>&1; then
    if ! json_err="$(python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$candidate" 2>&1)"; then
        echo "$json_err" >&2
        die "candidate is not valid JSON (python3 json.load failed)"
    fi
    info "structural JSON pre-filter passed (python3)"
elif command -v jq >/dev/null 2>&1; then
    if ! jq empty "$candidate" >/dev/null 2>&1; then
        die "candidate is not valid JSON (jq empty failed)"
    fi
    info "structural JSON pre-filter passed (jq)"
else
    info "no python3/jq available, skipping structural pre-filter (relying on -check)"
fi

# --- 3. Authoritative validation via tproxy-server's own parser ---

if ! check_stderr="$("$TPROXY_SERVER_BIN" -config "$TPROXY_CONFIG_PATH" -profiles-file "$candidate" -check 2>&1 >/dev/null)"; then
    echo "$check_stderr" >&2
    die "candidate failed tproxy-server -check validation"
fi
info "tproxy-server -check passed"

# --- 3b. Merge panel candidate with existing non-panel profiles ---

merged_tmp="$(mktemp "${BACKUP_DIR}/.profiles.merged.XXXXXX")"
tmp_merged="$merged_tmp"

merge_profiles() {
    local current_path="$1" candidate_path="$2" out_path="$3"
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$current_path" "$candidate_path" "$out_path" <<'PY'
import json, re, sys

current_path, candidate_path, out_path = sys.argv[1:4]
panel_name = re.compile(r"^user_\d+$")

candidate = json.load(open(candidate_path, encoding="utf-8"))
current = {"profiles": []}
if current_path and current_path != "-":
    try:
        with open(current_path, encoding="utf-8") as f:
            current = json.load(f)
    except FileNotFoundError:
        pass

foreign = [p for p in current.get("profiles", []) if not panel_name.match(p.get("name", ""))]
merged = {"profiles": foreign + candidate.get("profiles", [])}

names, secrets = set(), set()
for p in merged["profiles"]:
    name = p.get("name", "")
    secret = p.get("secret", "")
    if not name or not secret:
        print(f"{name!r}: profile missing name or secret", file=sys.stderr)
        sys.exit(1)
    if name in names:
        print(f"duplicate profile name {name!r}", file=sys.stderr)
        sys.exit(1)
    if secret in secrets:
        print(f"duplicate secret for profile {name!r}", file=sys.stderr)
        sys.exit(1)
    names.add(name)
    secrets.add(secret)

with open(out_path, "w", encoding="utf-8") as f:
    json.dump(merged, f, indent=2)
    f.write("\n")
PY
        return $?
    fi
    die "python3 is required to merge panel profiles with existing profiles.json (preserves non-panel entries like \"default\")"
}

current_for_merge="-"
if [ -e "$TPROXY_PROFILES_PATH" ]; then
    current_for_merge="$TPROXY_PROFILES_PATH"
fi

panel_candidate="$candidate"

if ! merge_profiles "$current_for_merge" "$candidate" "$merged_tmp"; then
    die "failed to merge candidate with existing profiles.json"
fi
candidate="$merged_tmp"
info "merged panel candidate with existing non-panel profiles ($(wc -c < "$candidate") bytes)"

if ! check_stderr="$("$TPROXY_SERVER_BIN" -config "$TPROXY_CONFIG_PATH" -profiles-file "$candidate" -check 2>&1 >/dev/null)"; then
    echo "$check_stderr" >&2
    die "merged profiles failed tproxy-server -check validation"
fi
info "tproxy-server -check passed on merged profiles"

# --- 4. Backup current live profiles.json ---

mkdir -p "$BACKUP_DIR"
chmod 0700 "$BACKUP_DIR"

backup_path=""
if [ -e "$TPROXY_PROFILES_PATH" ]; then
    ts="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
    backup_path="${BACKUP_DIR%/}/profiles.json.${ts}.bak"
    # Guard against two applies landing in the same second (unlikely but
    # possible under load): fall back to a numeric suffix rather than
    # silently overwriting an existing backup.
    if [ -e "$backup_path" ]; then
        n=1
        while [ -e "${BACKUP_DIR%/}/profiles.json.${ts}.${n}.bak" ]; do
            n=$((n + 1))
        done
        backup_path="${BACKUP_DIR%/}/profiles.json.${ts}.${n}.bak"
    fi
    cp -p -- "$TPROXY_PROFILES_PATH" "$backup_path"
    info "backed up current profiles.json to $backup_path"
else
    info "no existing profiles.json at $TPROXY_PROFILES_PATH, skipping backup (first run?)"
fi

# --- 5. Rotate backups, keep newest BACKUP_KEEP ---

if [ "$BACKUP_KEEP" -gt 0 ]; then
    # profiles.json.<timestamp>[.n].bak sorts lexically = chronologically
    # given the timestamp format above, so a plain sort on filenames is
    # sufficient without needing to stat mtimes.
    mapfile -t backups < <(find "$BACKUP_DIR" -maxdepth 1 -type f -name 'profiles.json.*.bak' -printf '%f\n' 2>/dev/null | sort)
    count="${#backups[@]}"
    if [ "$count" -gt "$BACKUP_KEEP" ]; then
        remove_count=$((count - BACKUP_KEEP))
        for ((i = 0; i < remove_count; i++)); do
            rm -f -- "${BACKUP_DIR%/}/${backups[$i]}"
        done
        info "rotated backups: removed $remove_count old file(s), kept $BACKUP_KEEP"
    fi
fi

# --- 6. Atomic install of the candidate as the new profiles.json ---

profiles_dir="$(dirname -- "$TPROXY_PROFILES_PATH")"
mkdir -p "$profiles_dir"

tmp_install="$(mktemp "${profiles_dir}/.profiles.json.XXXXXX")"
cp -- "$candidate" "$tmp_install"

if ! chown "$PROFILES_OWNER" "$tmp_install" 2>/dev/null; then
    die "failed to chown $tmp_install to $PROFILES_OWNER (must run as root)"
fi
if ! chmod "$PROFILES_MODE" "$tmp_install"; then
    die "failed to chmod $tmp_install to $PROFILES_MODE"
fi

mv -f -- "$tmp_install" "$TPROXY_PROFILES_PATH"
tmp_install=""
profile_count="?"
profile_names=""
if command -v python3 >/dev/null 2>&1; then
    profile_count="$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(len(d.get("profiles", [])))' "$TPROXY_PROFILES_PATH" 2>/dev/null || echo '?')"
    profile_names="$(python3 - "$TPROXY_PROFILES_PATH" "$panel_candidate" <<'PY' 2>/dev/null || true
import json, sys

installed_path, panel_candidate_path = sys.argv[1:3]
with open(installed_path, encoding="utf-8") as f:
    installed = json.load(f)
with open(panel_candidate_path, encoding="utf-8") as f:
    panel = json.load(f)

installed_names = {p.get("name") for p in installed.get("profiles", [])}
missing = []
for p in panel.get("profiles", []):
    name = p.get("name")
    if name and name not in installed_names:
        missing.append(name)
if missing:
    print("MISSING:" + ",".join(missing))
    sys.exit(1)
print(",".join(sorted(n for n in installed_names if n)))
PY
)"
    if [[ "$profile_names" == MISSING:* ]]; then
        die "installed profiles.json is missing panel profile(s): ${profile_names#MISSING:}"
    fi
fi
info "installed new profiles.json ($profile_count profile(s)) at $TPROXY_PROFILES_PATH${profile_names:+, names: $profile_names}"

# --- 7. Restart tproxy-server ---

if ! systemctl restart "$TPROXY_SERVICE_NAME"; then
    info "systemctl restart $TPROXY_SERVICE_NAME failed, attempting best-effort restore of previous profiles.json"
    if [ -n "$backup_path" ] && [ -e "$backup_path" ]; then
        tmp_restore="$(mktemp "${profiles_dir}/.profiles.json.XXXXXX")"
        if cp -- "$backup_path" "$tmp_restore" \
            && chown "$PROFILES_OWNER" "$tmp_restore" 2>/dev/null \
            && chmod "$PROFILES_MODE" "$tmp_restore"; then
            mv -f -- "$tmp_restore" "$TPROXY_PROFILES_PATH"
            tmp_restore=""
            info "restored previous profiles.json from $backup_path"
        else
            echo "${PROG}: WARNING: restore from $backup_path FAILED, live profiles.json may be inconsistent" >&2
        fi
    else
        info "no backup was taken (first run), nothing to restore"
    fi
    die "systemctl restart $TPROXY_SERVICE_NAME failed"
fi

info "restarted $TPROXY_SERVICE_NAME successfully"
exit 0
