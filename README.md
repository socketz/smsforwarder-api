<div align="center">
  <h1>smsforwarder-api</h1>
  <p>
    <strong>Multi-user backend for the SMS Forwarder app</strong>
  </p>
  <p>
    Receives SMS uploaded by the phone client, authenticates the user, and fans each
    message out to that user's configured channels (Telegram). A single Go binary with an
    embedded SQLite store and a built-in operator console — no external services.
  </p>
  <p>
    📱 Client: <a href="https://github.com/ShuttleLab/SMSForwarder">SMSForwarder</a>
    &nbsp;·&nbsp;
    🌐 <a href="https://smsforwarder.shuttlelab.org">SMS Forwarder — forward SMS to Telegram</a>
  </p>
</div>

<div align="center">

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)
![SQLite](https://img.shields.io/badge/SQLite-modernc%20(CGO--free)-003B57?style=flat-square&logo=sqlite)
![Telegram](https://img.shields.io/badge/Telegram-Bot-26A5E4?style=flat-square&logo=telegram)
![License](https://img.shields.io/badge/License-AGPL--3.0-blue?style=flat-square)

</div>

## About

smsforwarder-api is the server half of **SMS Forwarder**. The phone uploads each incoming
SMS to `POST /api/sms`; the server authenticates the device, stores the message briefly,
and delivers it to the user's channels. Channel configuration lives here (not in the app),
so channels can be added or changed without rebuilding the client.

It is **relay-and-forget**: an SMS body lives in the outbox only until it is delivered (or
finally given up), then it is deleted. Only metadata is kept in the delivery log, visible
to the operator. Modeled on the sibling `secretary` project — one binary, pure-Go SQLite
(no CGO), no message queue, no ORM.

The **official instance is operated by ShuttleLab** at `smsforwarder-api.shuttlelab.org`, and
the released app talks to it out of the box. Because the code is open source (AGPL-3.0),
anyone can also run their own instance.

## Key Features

- **Multi-user** — open registration; every user's data is isolated (all queries scoped by `user_id`). A message can never be delivered to the wrong user.
- **Device-token auth** — sign in once with username + password; the server issues a long-lived, per-device token (stored only as a SHA-256 hash) that the app sends as a Bearer credential. Tokens are individually revocable.
- **Telegram channel** — one shared bot handles every user. Users bind by tapping a deep link and pressing Start (`/bind` flow); the bot writes their `chat_id` straight into the store. No tokens to copy.
- **Durable delivery** — an outbox with an exponential-backoff retry engine survives restarts; long messages are auto-split under Telegram's 4096-char limit; delivery is idempotent via a per-message key.
- **Self-healing bindings** — when Telegram reports a recipient is unreachable (403: bot blocked / can't initiate), the binding is cleared automatically so the app prompts the user to re-bind. There is also a one-click "reset all Telegram bindings" for when the shared bot is replaced.
- **Operator admin console** — an embedded web UI (session-cookie login) for delivery logs, user management (ban / revoke devices / delete), per-table data maintenance, and bot settings. Binds to localhost by default.
- **No cloud dependencies** — no GMS/FCM, no external DB. A single binary plus one SQLite file.

## Tech Stack

- **Language:** Go 1.25
- **Storage:** `modernc.org/sqlite` (pure Go, no CGO), WAL mode
- **Telegram:** `github.com/go-telegram/bot` (long polling)
- **HTTP / admin UI:** standard library `net/http` + `html/template`
- **Passwords:** bcrypt (`golang.org/x/crypto`)

## Getting Started

### Prerequisites

- Go 1.25+

### Run

```bash
cp .env.example .env      # optional; sane defaults otherwise
go run .                  # API on HTTP_ADDR (default 0.0.0.0:8090)
```

Configuration (env, see `.env.example`): `DB_PATH`, `HTTP_ADDR`, `RATE_LIMIT_PER_MIN`,
`BOT_TOKEN`, `BOT_USERNAME`, `ADMIN_ADDR`, `ADMIN_USER`, `ADMIN_PASSWORD`. The Telegram bot
token/username may also be set in the admin console (DB overrides env; a changed token
needs a restart).

### API

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/healthz` | – | liveness |
| POST | `/api/register` | – | `{username, password}` |
| POST | `/api/login` | – | `{username, password, device_label?}` → `{device_token}` |
| POST | `/api/logout` | Bearer | revoke current device |
| GET | `/api/channels` | Bearer | list |
| POST | `/api/channels` | Bearer | `{type, config, enabled?}` |
| GET/PATCH/DELETE | `/api/channels/{id}` | Bearer | read / edit / delete |
| POST | `/api/channels/{id}/test` | Bearer | send a test message now |
| POST | `/api/channels/{id}/bind-code` | Bearer | Telegram bind code + deep link |
| POST | `/api/sms` | Bearer | `{sender, content, timestamp(ms), client_msg_id}` |

Send `Authorization: Bearer <device_token>` (returned once at login).
Channel `config` per type: `telegram` → `{"chat_id":<int>}` (filled by `/bind`).

### Admin console

Enabled when `ADMIN_USER` + `ADMIN_PASSWORD` are set; served on `ADMIN_ADDR`
(default `127.0.0.1:8091`). Keep it on localhost and reach it over an SSH tunnel:

```bash
ssh -L 8091:127.0.0.1:8091 <server>   # then open http://127.0.0.1:8091/admin
```

## Deploy

Single binary + SQLite, same host as `secretary`. Expose it with a **Cloudflare Tunnel**
(no public inbound port): add an `ingress` rule mapping `smsforwarder-api.shuttlelab.org`
to `http://127.0.0.1:8090`, then run behind systemd.

```bash
go build -o smsforwarder-api .
# HTTP_ADDR=127.0.0.1:8090 (localhost only; the tunnel reaches it)
sudo systemctl restart smsforwarder-api
curl -s https://smsforwarder-api.shuttlelab.org/healthz   # {"ok":true}
```

### Deploy with Docker

A `Dockerfile` + `compose.yaml` are included. The image builds the binary
directly from the upstream source (`go install github.com/socketz/smsforwarder-api@main`
in the builder stage — no git clone, no local checkout is copied into the image;
pin a commit with `docker compose build --build-arg MODULE_REF=<commit>`), runs
hardened as a non-root user, keeps the SQLite database on a named volume, and
publishes only the public API loopback-only on the host. Configure everything
from a project-root `.env` (all 8 variables from `.env.example`; compose
interpolation falls back to the same defaults, so `.env` is optional).

```bash
cp .env.example .env   # edit BOT_TOKEN, BOT_USERNAME, ADMIN_USER, ADMIN_PASSWORD
docker compose up -d
curl -s http://127.0.0.1:8090/healthz   # {"ok":true}
```

Notes:

- `DB_PATH` is forced to `/data/smsforwarder.db` inside the container; the host
  `.env` value for it is ignored. Data lives in the named volume `smsforwarder-data`
  — back it up (`docker compose down` then copy the volume, or `docker run --rm -v
  smsforwarder-data:/data -v $PWD:/backup alpine tar czf /backup/data.tgz -C /data .`).
- Only port 8090 is published, bound to `127.0.0.1` (reverse proxy / tunnel on the
  host terminates TLS). The admin console (8091) is **not** published; reach it via
  `docker compose exec api wget -qO- http://127.0.0.1:8091/admin/login`. Never set
  `ADMIN_ADDR` to `0.0.0.0` unless it is fronted by TLS — the console is plaintext
  HTTP.
- `docker compose logs -f api` for logs; `docker compose restart api` applies config
  and env changes.

## License

Licensed under the **GNU Affero General Public License v3.0** (AGPL-3.0) — see
[LICENSE](LICENSE). You may use, study, modify and redistribute it, but any distributed
or network-served derivative (including a hosted instance) must also be released under
AGPL-3.0.

<div align="center">
  Built by <a href="https://github.com/ShuttleLab">ShuttleLab</a>
</div>
