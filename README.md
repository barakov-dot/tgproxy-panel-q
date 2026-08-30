# tgproxy-panel

Web panel + Telegram bot for issuing and revoking user access to an existing
[tproxy-server](https://github.com/telegramdesktop/tproxy-server) (Telegram web proxy)
installation. It does not modify tproxy-server's own logic — it only manages
`profiles.json` entries and restarts the service.

Status: under active development. Installation instructions will land here once
`deploy/install.sh` is ready — see [CLAUDE.md](CLAUDE.md) for the architecture
and build plan in the meantime.
