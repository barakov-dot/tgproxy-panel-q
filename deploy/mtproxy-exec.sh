#!/usr/bin/env bash
#
# mtproxy-exec.sh — MTProxy ExecStart wrapper for tgproxy-panel.
#
# MTProxy accepts multiple secrets only as separate `-S` flags on the command
# line (see telegrammessenger/mtproxy README). This wrapper reads one secret
# per line from /etc/mtproxy/mtproxy.secrets and passes each as `-S`. Falls
# back to a single MTPROXY_SECRET from /etc/mtproxy/mtproxy.env when the list
# file is missing or empty.
#
# Installed to /opt/tgproxy-panel/bin/mtproxy-exec.sh by deploy/install.sh;
# referenced from systemd drop-in mtproxy.service.d/tgproxy-panel.conf.

set -euo pipefail

ENV_FILE="/etc/mtproxy/mtproxy.env"
SECRETS_FILE="${MTPROXY_SECRETS_FILE:-/etc/mtproxy/mtproxy.secrets}"
MT_BIN="${MTPROXY_BIN:-/opt/MTProxy/objs/bin/mtproto-proxy}"
STATS_PORT="${MTPROXY_STATS_PORT:-8888}"
LISTEN_PORT="${MTPROXY_LISTEN_PORT:-2398}"

if [ ! -x "$MT_BIN" ]; then
    echo "mtproxy-exec.sh: MTProxy binary not found: $MT_BIN" >&2
    exit 1
fi

if [ ! -r "$ENV_FILE" ]; then
    echo "mtproxy-exec.sh: missing $ENV_FILE" >&2
    exit 1
fi

# shellcheck source=/dev/null
. "$ENV_FILE"

secret_args=()
if [ -s "$SECRETS_FILE" ]; then
    while IFS= read -r secret || [ -n "$secret" ]; do
        secret="${secret%%#*}"
        secret="$(printf '%s' "$secret" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')"
        [ -n "$secret" ] || continue
        secret_args+=(-S "$secret")
    done < "$SECRETS_FILE"
fi

if [ "${#secret_args[@]}" -eq 0 ]; then
    if [ -z "${MTPROXY_SECRET:-}" ]; then
        echo "mtproxy-exec.sh: no secrets in $SECRETS_FILE and MTPROXY_SECRET unset" >&2
        exit 1
    fi
    # mtproxy.env allows exactly one secret; ignore accidental trailing tokens.
    secret="$(printf '%s' "$MTPROXY_SECRET" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')"
    secret_args=(-S "$secret")
fi

exec "$MT_BIN" -u mtproxy -p "$STATS_PORT" -H "$LISTEN_PORT" \
    "${secret_args[@]}" \
    --aes-pwd /etc/mtproxy/proxy-secret /etc/mtproxy/proxy-multi.conf \
    -M "${MTPROXY_WORKERS:-1}" -C "${MTPROXY_MAX_CONNECTIONS:-4096}"
