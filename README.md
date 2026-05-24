# logo-service

Self-hosted Go microservice that resolves stock ticker symbols to PNG company logos. Built to back the [dividend-portfolio](https://github.com/fleveque/dividend-portfolio) app's `<StockLogo>` component and replace third-party logo CDNs (Brandfetch / Clearbit).

[![CI](https://github.com/fleveque/logo-service/actions/workflows/ci.yml/badge.svg)](https://github.com/fleveque/logo-service/actions/workflows/ci.yml)
[![Deploy](https://github.com/fleveque/logo-service/actions/workflows/deploy.yml/badge.svg)](https://github.com/fleveque/logo-service/actions/workflows/deploy.yml)

## Services Architecture

```
                         Internet
                            |
              +-------------+-------------+
              |             |             |
        quantic.es    pulse.quantic.es  logos.quantic.es
              |             |             |
    +---------+--+  +-------+------+  +---+--------+
    |  Rails App |  |    Pulse     |  | logo-      |
    |            |  |   Phoenix    |  | service    |
    | - Auth     |  |   LiveView   |  | (this, Go) |
    | - Radar    |  |              |  |            |
    | - Holdings |  | - Public     |  | - Logo     |
    | - Buy Plan |  |   portfolios |  |   pipeline |
    +-----+------+  | - Community  |  | - SQLite   |
          |         |   dashboard  |  |   cache    |
          +---+  +--+------+-------+  +------------+
              |  |
          +---+--+---+
          |   NATS   |
          | JetStream|
          +----------+
```

The Rails app fetches a logo via `GET /api/v1/logos/:symbol?...` whenever it renders a stock card. The first request for an unknown ticker runs through the acquisition pipeline; every subsequent request hits the local filesystem + SQLite cache.

## The 3-layer pipeline

```
GET /api/v1/logos/REP.MC?size=m&company_name=Repsol,%20S.A.

  ─► Layer 1 · Cache         ─►  return cached PNG (fast path)
       (FS + SQLite)              ↓ miss
  ─► Layer 2 · GitHub repos  ─►  download, resize, cache, return
       (ticker_icons/*.png)       ↓ miss (very common for non-US)
  ─► Layer 3 · LLM           ─►  ask Gemini/Claude/OpenAI for the
       (web search + JSON)        official logo URL, download,
                                  resize, cache, return
                                  ↓ all providers exhausted
  ─► 404 "no provider found"
```

- **Layer 2** scans configured GitHub repos (`davidepalazzo/ticker-logos`, `nvstly/icons`) — covers ~5000 US tickers and some major international ones.
- **Layer 3** is the safety net for everything else. `company_name` is forwarded as a hint so the LLM can disambiguate exchange-suffixed tickers like `REP.MC` (Repsol), `DGE.L` (Diageo), `IBE.MC` (Iberdrola) it might not recognise from training data alone.

Providers are tried in `llm.provider_order` (default: `gemini` → `anthropic` → `openai`). A provider is skipped if its API key isn't configured, so a deploy with just `LOGO_LLM_GEMINI_API_KEY` set works out of the box on the free tier.

## API

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/healthz` | none | Liveness check |
| `GET` | `/api/v1/logos/:symbol` | API key | Fetch a logo PNG |
| `GET` | `/api/v1/admin/stats` | Admin key | Pipeline + cache stats |
| `POST` | `/api/v1/admin/import` | Admin key | Bulk-import logos from GitHub repos |

### `GET /api/v1/logos/:symbol`

```
GET /api/v1/logos/AAPL?size=m&api_key=...
GET /api/v1/logos/REP.MC?size=m&api_key=...&company_name=Repsol%2C%20S.A.
GET /api/v1/logos/AAPL?size=s&bg=ffffff&api_key=...
```

Query params:

| Param | Default | Notes |
|---|---|---|
| `size` | `m` | One of `xs` / `s` / `m` / `l` / `xl` (16 / 32 / 64 / 128 / 256 px) |
| `bg` | none | Hex color (`ffffff`, `0d1117`); flattens transparency for that background |
| `api_key` | required | Sent as query param so it works inside `<img src>` tags. `X-API-Key` header also accepted |
| `company_name` | optional | Hint used only on cache misses falling through to the LLM layer. Cache key is symbol-only, so the hint never changes a cached result |

Response: `image/png` bytes with `Cache-Control: public, max-age=86400`.

## Configuration

Config can come from `config.yaml`, environment variables (`LOGO_*` prefix), or both. Env vars override file values. See `config.example.yaml` for the full layout.

| Env var | Required | Purpose |
|---|---|---|
| `LOGO_AUTH_API_KEYS` | yes | Comma-separated list of accepted API keys |
| `LOGO_AUTH_ADMIN_KEYS` | yes | Comma-separated list of admin-only keys |
| `LOGO_LLM_GEMINI_API_KEY` | optional¹ | Gemini key (free-tier compatible — primary LLM by default) |
| `LOGO_LLM_ANTHROPIC_API_KEY` | optional¹ | Claude key (fallback) |
| `LOGO_LLM_OPENAI_API_KEY` | optional¹ | OpenAI key (fallback) |
| `LOGO_STORAGE_DATABASE_PATH` | no | SQLite path (default `./storage/logo-service.db`) |
| `LOGO_STORAGE_LOGO_DIR` | no | Filesystem cache (default `./storage/logos`) |
| `LOGO_CORS_ALLOWED_ORIGINS` | no | Comma-separated origins for the API |
| `LOGO_LOG_LEVEL` | no | `info` (default) or `debug` |

¹ With **none** of the LLM keys set, the service still runs but Layer 3 is disabled — non-US tickers fall through to a 404.

## Development

**Prerequisites:** Go 1.24+, libvips (for image resizing), SQLite. On macOS: `brew install vips`. On Debian/Ubuntu: `apt-get install libvips-dev`.

```bash
cp config.example.yaml config.yaml    # then edit auth keys + LLM API keys
make run                              # localhost:8080
make test                             # full test suite
make fmt vet                          # format + static analysis
```

The `cmd/cli` binary (`make cli ARGS="import --source github"`) drives one-shot imports without booting the HTTP server.

### Architecture

```
cmd/
├── server/           HTTP entrypoint
└── cli/              one-shot admin commands
internal/
├── handler/          Gin handlers (logo, admin, health)
├── service/          LogoService — orchestrates the 3-layer pipeline
├── provider/         GitHub + LLM acquisition providers
├── llm/              Provider-agnostic LLM clients (gemini, anthropic, openai)
├── storage/          SQLite (metadata) + filesystem (PNG bytes)
├── middleware/       API-key auth, rate limit, CORS
├── model/            DB models
└── config/           Viper-based config loader
```

The pipeline is wired manually in `cmd/server/main.go` — no DI framework. Each provider can be nil; the service skips layers that aren't configured.

## Deployment

Deployed via [Kamal](https://kamal-deploy.org/) to the same Hetzner ARM64 VPS as the Rails and Pulse apps.

| Environment | URL | Branch | Deploy |
|---|---|---|---|
| Production | `logos.quantic.es` | `main` | Auto via GitHub Actions |

### Auto-deploy

`.github/workflows/deploy.yml` runs `kamal deploy` whenever the CI workflow completes successfully on `main` (chained via `workflow_run`). The Go binary is cross-compiled to ARM64 inside Docker (slow on the first deploy because buildx cache is cold; subsequent builds reuse layers).

To deploy manually:

```bash
mise exec ruby@3.4.1 -- kamal deploy
```

(Kamal is a Ruby gem; `.tool-versions` pins `ruby 3.4.1` alongside Go so the `kamal` shim resolves locally without `mise use -g ruby@3.4.1`.)

### GitHub Secrets

The deploy workflow needs these repo-level secrets:

| Secret | Purpose |
|---|---|
| `SSH_PRIVATE_KEY` | Private key for SSH access to the VPS (same key as Pulse / dividend-portfolio) |
| `BW_ACCOUNT` | Your Bitwarden login email (kept out of git) |
| `BW_CLIENTID` | Bitwarden personal API key — client_id (Account Settings → Security → Keys) |
| `BW_CLIENTSECRET` | Bitwarden personal API key — client_secret |
| `BW_PASSWORD` | Bitwarden master password — used to unlock the vault non-interactively in CI |

`.kamal/secrets` pulls auth keys + the shared Gemini key from a single
Bitwarden Secure Note item (`quantic-prod`) holding the values as custom
fields. The deploy workflow installs the `bw` CLI via npm, configures the
EU server, logs in with `bw login --apikey`, unlocks with
`bw unlock --raw --passwordenv BW_PASSWORD`, and exports the resulting
`BW_SESSION` to `$GITHUB_ENV` — kamal then inherits the session and
skips its own login attempt.

For local Kamal commands, copy the sample envs and let `direnv` load them:

```bash
cp env.sample .env       # fill in BW_CLIENTID + BW_CLIENTSECRET + BW_PASSWORD
cp envrc.sample .envrc
direnv allow
```

#### Switching back to 1Password

The 1Password version is preserved verbatim at `.kamal/secrets.1password.example`.
To roll back: `cp .kamal/secrets.1password.example .kamal/secrets`, then
revert the BW install + env block in `.github/workflows/deploy.yml`.
The 1P vault was never touched — all secrets are still there.

### First-time setup on a fresh VPS

```bash
mise exec ruby@3.4.1 -- kamal setup
```

Provisions the host, installs Docker if needed, deploys the first container, and sets up `kamal-proxy` for SSL + multi-app routing.
