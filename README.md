# tgproxy-panel

Web panel + Telegram bot for managing users of
[tproxy-server](https://github.com/telegramdesktop/tproxy-server).

Issues and revokes per-user proxy secrets in `profiles.json`; does not modify
tproxy-server/MTProxy logic itself.

## Requirements

- Linux x86_64 (Debian 13 / Ubuntu 24 tested target)
- [tproxy-server](https://github.com/telegramdesktop/tproxy-server) already installed via its `deploy/install.sh`
- Caddy with TLS on the same hostname as the proxy
- systemd

## Quick install

```bash
git clone https://github.com/barakov-dot/tgproxy-panel-q.git
cd tgproxy-panel-q
sudo ./deploy/install.sh
```

The installer downloads the latest release binary (or builds from source if Go is present),
patches Caddyfile, creates systemd unit, and prints the secret panel URL.

See [PLAN.md](PLAN.md) for full specification and [ARCHITECTURE.md](ARCHITECTURE.md) for
implementation conventions.

## Development

```bash
cp .env.example .env   # fill in values for local dev
go test ./...
go build -o tgproxy-panel ./cmd/tgproxy-panel
```

Cross-compile for production target:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o tgproxy-panel-linux-amd64 ./cmd/tgproxy-panel
```

## License

MIT — see [LICENSE](LICENSE).
