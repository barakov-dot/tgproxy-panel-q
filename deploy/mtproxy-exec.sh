#!/usr/bin/env bash
#
# mtproxy-exec.sh — MTProxy ExecStart wrapper for tgproxy-panel.
#
# Sources /etc/mtproxy/mtproxy.env and passes each secret from MTPROXY_SECRETS
# (space-separated) as `-S` to the official mtproto-proxy binary. Falls back to
# a single MTPROXY_SECRET when MTPROXY_SECRETS is unset.
#
# Installed to /opt/tgproxy-panel/bin/mtproxy-exec.sh by deploy/install.sh;
# referenced from systemd drop-in mtproxy.service.d/tgproxy-panel.conf.

set -euo pipefail

ENV_FILE="/etc/mtproxy/mtproxy.env"
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
if [ -n "${MTPROXY_SECRETS:-}" ]; then
    for secret in $MTPROXY_SECRETS; do
        secret="$(printf '%s' "$secret" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')"
        [ -n "$secret" ] || continue
        secret_args+=(-S "$secret")
    done
fi

if [ "${#secret_args[@]}" -eq 0 ]; then
    if [ -z "${MTPROXY_SECRET:-}" ]; then
        echo "mtproxy-exec.sh: MTPROXY_SECRETS and MTPROXY_SECRET unset in $ENV_FILE" >&2
        exit 1
    fi
    secret_args=(-S "$MTPROXY_SECRET")
fi

exec "$MT_BIN" -u mtproxy -p "$STATS_PORT" -H "$LISTEN_PORT" \
    "${secret_args[@]}" \
    --aes-pwd /etc/mtproxy/proxy-secret /etc/mtproxy/proxy-multi.conf \
    -M "${MTPROXY_WORKERS:-1}" -C "${MTPROXY_MAX_CONNECTIONS:-4096}"
