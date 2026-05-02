# Nexus

Nexus is a self-hosted middleware layer that connects any HTTP or gRPC API and exposes all of them through a single consistent endpoint. Drop a YAML file per API, run `make run`, and every connector is immediately callable through `POST /call`.

# Why?

I kept writing the same glue code — auth injection, retry logic, response normalization — for every API I integrated. Nexus moves that logic into connector definitions so application code never has to care about the upstream API's quirks. You describe the connector once; everywhere else you just call Nexus.

## Quick start

```bash
git clone https://github.com/ullamua/nexus
cd nexus
bash install.sh         # builds binary + creates .env from .env.example
# Edit .env — set NEXUS_VAULT_KEY to a 32+ char secret (openssl rand -hex 32)
make run
```

Or manually:

```bash
make setup              # creates .env from .env.example
# edit .env → set NEXUS_VAULT_KEY
make run                # builds + starts (auto-loads .env)
```

```bash
curl http://localhost:8080/health
curl http://localhost:8080/registry
curl -X POST http://localhost:8080/call \
  -H "Content-Type: application/json" \
  -d '{"connector":"animetsu","action":"trending","params":{}}'
```

## Make targets

| Target | Description |
|--------|-------------|
| `make build` | Compile the binary to `bin/nexus` |
| `make run` | Build + start (auto-loads `.env`) |
| `make dev` | Start with `debug` logging, tracing, and dashboard enabled |
| `make setup` | Create `.env` from `.env.example` (safe to run multiple times) |
| `make test` | Run unit tests |
| `make live` | Run E2E tests against live APIs |
| `make check` | vet + test |
| `make release` | Cross-compile for linux/amd64, linux/arm64, darwin/arm64, darwin/amd64, windows/amd64 |
| `make install` | Install binary to `~/.local/bin/nexus` |
| `make zip` | Package nexus into a versioned zip in `dist/` |
| `make docker` | Build Docker image |
| `make lint` | Run golangci-lint |
| `make clean` | Remove `bin/`, `static/`, `dist/` |

## API endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | none | Connector health statuses |
| GET | `/ready` | none | Readiness probe with connector count |
| GET | `/version` | none | Version info |
| GET | `/metrics` | none | Prometheus metrics |
| GET | `/registry` | api key | List all registered connectors |
| GET | `/registry/{name}` | api key | Full connector schema |
| POST | `/call` | api key | Call a single connector action |
| POST | `/pipeline` | api key | Execute a DAG pipeline |
| POST | `/pipeline/dry-run` | api key | Validate pipeline without making any calls |
| POST | `/resolve` | api key | Map a natural-language intent → connector + action |
| GET | `/diagnostics/traces` | admin key | Last 100 request traces |
| POST | `/vault/set` | admin key | Store an encrypted credential |
| POST | `/admin/reload` | admin key | Hot-reload connector YAML files without restart |

## Connector YAML format

```yaml
name: stripe
version: "1.0.0"
description: Stripe payment processing API
protocol: rest
base_url: https://api.stripe.com/v1

auth:
  type: bearer
  env: STRIPE_SECRET_KEY   # env var name — never put the value here

rate_limit:
  requests_per_second: 100
  burst: 20

timeout_ms: 10000

headers:
  Stripe-Version: "2024-06-20"

actions:
  create_customer:
    method: POST
    path: /customers
    description: Create a new Stripe customer
    input_schema:
      email:
        type: string
        required: true
    output_map:
      id: customer_id       # renames upstream field → your normalized name
    cache: false

  get_customer:
    method: GET
    path: /customers/{{params.customer_id}}
    response_root: ""       # optional: unwrap a top-level JSON key before output_map
    cache: true
    cache_ttl_seconds: 60
```

### response_root

Some APIs nest their payload under a key like `"data"` or `"result"`. Set `response_root` to unwrap it automatically:

```yaml
# upstream returns: {"success": true, "data": {"id": "abc", "title": "Naruto"}}
response_root: data
# nexus now sees: {"id": "abc", "title": "Naruto"} — output_map applies to this
```

Supports dot notation for one level of nesting: `response_root: data.results`

## Calling connectors

### Single action

```bash
curl -X POST http://localhost:8080/call \
  -H "Content-Type: application/json" \
  -H "X-Nexus-Key: your-api-key" \
  -d '{
    "connector": "stripe",
    "action": "create_customer",
    "params": {"email": "user@example.com"},
    "options": {"cache": true, "cache_ttl_seconds": 60}
  }'
```

Response:

```json
{
  "ok": true,
  "connector": "stripe",
  "action": "create_customer",
  "data": {"customer_id": "cus_123"},
  "meta": {
    "request_id": "cm4x...",
    "latency_ms": 142,
    "cached": false,
    "connector_version": "1.0.0",
    "timestamp": "2024-01-01T00:00:00Z"
  }
}
```

### DAG pipeline

Steps with no shared dependencies run concurrently. `{{step.data.field}}` templates are resolved right before each step executes. Array responses are available via indexed templates — `{{search.data.results.0.id}}` resolves to the `id` field of the first result.

```bash
curl -X POST http://localhost:8080/pipeline \
  -H "Content-Type: application/json" \
  -d '{
    "input": {"q": "attack on titan", "ep": "1"},
    "pipeline": [
      {
        "id": "search",
        "connector": "animetsu",
        "action": "search",
        "params": {"q": "{{input.q}}"}
      },
      {
        "id": "watch",
        "connector": "animetsu",
        "action": "watch",
        "params": {
          "id": "{{search.data.results.0.id}}",
          "ep": "{{input.ep}}"
        },
        "depends_on": ["search"]
      }
    ]
  }' | jq '{hit: .steps.search.data.results[0].title, watch: .steps.watch.data}'
```

> **Array responses:** when an upstream API returns an array under a key (e.g. `response_root: data`), Nexus normalises it to `{"results": [...]}`. Access elements with `.data.results[N]` in jq and `{{step.data.results.N.field}}` in templates.

### Dry-run a pipeline (no calls made)

```bash
curl -X POST http://localhost:8080/pipeline/dry-run \
  -H "Content-Type: application/json" \
  -d '{
    "input": {"query": "naruto"},
    "pipeline": [
      {"id": "step1", "connector": "animetsu", "action": "search", "params": {"q": "{{input.query}}"}},
      {"id": "step2", "connector": "animetsu", "action": "anime_detail", "depends_on": ["step1"]}
    ]
  }'
```

Returns a `DryRunResult` with:
- `ok` — whether all checks passed
- `issues` — list of problems with step/field/code/message
- `execution_order` — computed DAG execution order
- `summary` — human-readable summary

Validates: connector existence, action existence, depends_on references, DAG cycles, forward template references.

### Semantic intent resolution

```bash
curl -X POST http://localhost:8080/resolve \
  -H "Content-Type: application/json" \
  -d '{"intent": "search for anime by name"}'
```

```json
{
  "ok": true,
  "connector": "animetsu",
  "action": "search",
  "score": 0.91,
  "confident": true
}
```

Uses character n-gram embeddings + cosine similarity across all connector action descriptions. `confident: true` when score ≥ 0.6.

### Hot-reload connectors

Add a new YAML file to `connectors.d/` and reload without restarting:

```bash
curl -X POST http://localhost:8080/admin/reload \
  -H "X-Nexus-Key: your-admin-key"
```

## Credentials and vault

Never put credentials in connector YAML files. Store them encrypted in the vault:

```bash
# Via API (requires admin key)
curl -X POST http://localhost:8080/vault/set \
  -H "X-Nexus-Key: your-admin-key" \
  -H "Content-Type: application/json" \
  -d '{"connector": "stripe", "key": "STRIPE_SECRET_KEY", "value": "sk_live_..."}'
```

Or set the env var directly — Nexus checks the vault first, then falls back to env vars.

The vault is AES-256-GCM encrypted, key-derived via Argon2id from `NEXUS_VAULT_KEY`.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `NEXUS_VAULT_KEY` | — | **Required.** 32+ char secret for vault encryption |
| `NEXUS_PORT` | `8080` | Port to listen on |
| `NEXUS_API_KEY` | — | Key required for `/call`, `/pipeline`, `/registry` (blank = open) |
| `NEXUS_ADMIN_KEY` | — | Key required for `/vault/set`, `/diagnostics/traces`, `/admin/reload` |
| `NEXUS_CONNECTORS_DIR` | `./connectors.d` | Directory with connector YAML files |
| `NEXUS_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `NEXUS_TRACE` | `false` | Include step traces in pipeline responses |
| `NEXUS_DASHBOARD` | `false` | Serve the embedded dashboard at `/dashboard` (`make run` enables this automatically) |
| `NEXUS_MAX_BODY_MB` | `1` | Max request body size in MB |
| `REDIS_URL` | — | Redis URL; falls back to in-process LRU if unset |
| `NEXUS_RATE_LIMIT_GLOBAL` | `0` | Global req/sec cap per IP (0 = unlimited) |

## Adding a new connector

1. Copy `connectors.d/_template.yaml` to `connectors.d/your_api.yaml`
2. Fill in `name`, `base_url`, `auth`, and at least one action
3. Add the env var name to `auth.env` — never put the secret value in the YAML
4. Set the credential: `POST /vault/set` or set the env var
5. Hot-reload: `POST /admin/reload` or restart Nexus

## Deploying

### Docker

```bash
make docker
docker run -p 8080:8080 \
  -e NEXUS_VAULT_KEY=your-secret \
  -v $(pwd)/connectors.d:/connectors.d \
  nexus:latest
```

With Redis:

```bash
docker-compose -f deploy/docker-compose.yml up
```

### Fly.io

```bash
fly launch --name nexus --region sin
fly secrets set NEXUS_VAULT_KEY=$(openssl rand -hex 32)
fly deploy
```

### Railway

```bash
railway login && railway init && railway up
railway variables set NEXUS_VAULT_KEY=$(openssl rand -hex 32)
```

### Render

Use `deploy/render.yaml`. Set `NEXUS_VAULT_KEY` in the Render dashboard.

### VPS / bare metal

```bash
make install                          # copies bin/nexus to ~/.local/bin/nexus
# or:  sudo cp bin/nexus /usr/local/bin/nexus
```

Systemd unit (`/etc/systemd/system/nexus.service`):

```ini
[Unit]
Description=Nexus API Protocol Engine
After=network.target

[Service]
Type=simple
User=nexus
WorkingDirectory=/opt/nexus
EnvironmentFile=/opt/nexus/.env
ExecStart=/usr/local/bin/nexus
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now nexus
```

### Behind Nginx

```nginx
location / {
    proxy_pass http://localhost:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_read_timeout 60s;
}
```

## Architecture

```
nexus/
├── engine/        — chi router, gateway, dispatcher, DAG pipeline executor,
│                    dry-run validator, semantic resolver
├── connectors/    — YAML registry (with hot-reload), HTTP runner,
│                    gRPC runner, field transformer, AES-256-GCM vault
├── intelligence/  — character n-gram embeddings, cosine similarity,
│                    semantic field mapper, intent resolver
├── cache/         — unified interface over in-process LRU or Redis
├── diagnostics/   — startup health probes, circular request trace buffer,
│                    Prometheus metrics (isolated registry per instance)
├── definitions/   — shared Go structs (ConnectorDef, CallRequest, envelopes)
├── connectors.d/  — your connector YAML files (animetsu, animekai, stripe, github)
├── deploy/        — Dockerfile, docker-compose, fly.toml, railway.json, render.yaml
└── tests/         — unit + integration + live E2E tests
```

## Performance

- Simple connector calls: ~5,000 req/sec on a single-core VPS (bottleneck is upstream network).
- 3-step sequential pipeline: ~1,000 req/sec.
- Parallel pipeline steps: `max(latency1, latency2)` not `latency1 + latency2`.
- Use Redis when running multiple instances behind a load balancer (shared cache).

## Limitations

- No OAuth 2.0 token refresh — store tokens manually after exchange.
- gRPC uses a generic JSON pass-through — generate a proto client for production.
- No built-in per-request retry — add it to `connectors/runner.go` if needed.

## License

MIT
