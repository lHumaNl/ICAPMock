# icap-mock

> A production-ready ICAP mock server for load testing and integration testing of systems that communicate over the ICAP protocol (RFC 3507).

![Go version](https://img.shields.io/badge/go-1.25-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Build](https://img.shields.io/badge/build-passing-brightgreen)

---

## Features

- **Multi-server mode** — run multiple independent ICAP servers in a single process, each on its own port with its own scenario set
- **Scenario engine v2** — YAML-based scenario files with `defaults`, `when` / `when_http` (matching ICAP headers and the encapsulated HTTP request/response), `set` (header overrides), and `delay` ranges
- **Two-layer response shaping** — `set:` / `body:` target the ICAP envelope, `http_set:` / `http_body:` target the encapsulated HTTP message (headers and body the origin client sees). Used together with `http_status:` to synthesize block pages with correct HTTP headers and chunked body.
- **Weighted responses** — probabilistic response selection within a scenario (`responses:` with `weight:`)
- **Response templates** — reusable named responses in `defaults.response_templates`, referenced via `use: <name>` from scenarios, branches, or weighted variants; `defaults.use:` acts as a file-wide fallback
- **Branches** — `branches:` list inside a scenario for OR-style dispatch with per-branch response (inline, `use:`, or weighted); first match wins; falls through to the next scenario if none match
- **Path captures** — endpoints like `/env/{id}/status` extract `{id}` from the URI; captured values are available as `${id}` in body/set/http_headers
- **Multi-method / multi-endpoint per port** — `method:` and `endpoint:` accept a scalar or a list; a single ICAP listener serves them all
- **Regex matching** — match headers, URLs, and other fields against regular expressions (`re:` prefix)
- **Rate limiting** — sharded token-bucket rate limiter configurable per server
- **Prometheus metrics** — expose request counts, latencies, and error rates at `/metrics`
- **Health checks** — HTTP `/health` and `/ready` endpoints for readiness probing
- **Interactive TUI** — terminal dashboard built with Bubbletea for live server monitoring
- **Request replay** — record and replay captured ICAP requests for regression testing
- **Hot-reload** — scenario files are watched and reloaded without restarting the server

---

## Quick Start

### Build from Source

Requires Go 1.25+.

```bash
git clone https://github.com/lHumaNl/ICAPMock.git
cd icapmock
make build
./bin/icap-mock server --config configs/example.yaml
```

### Docker

```bash
docker build -t icap-mock:latest .

docker run \
  -p 1344:1344 \
  -p 8080:8080 \
  -p 9090:9090 \
  -v $(pwd)/configs:/app/configs:ro \
  icap-mock:latest --config configs/example.yaml
```

Ports:
| Port | Purpose |
|------|---------|
| `1344` | ICAP protocol |
| `8080` | Health checks (`/health`, `/ready`) |
| `9090` | Prometheus metrics (`/metrics`) |

### Docker Compose

```bash
# Start the mock server
docker-compose up -d

# Start with monitoring stack (Prometheus + Grafana)
docker-compose --profile monitoring up -d
```

---

## Configuration

The server supports two modes, selected by the top-level structure of the YAML config file.

### Single-server mode

```yaml
server:
  host: "0.0.0.0"
  port: 1344
  scenarios_dir: "./configs/scenarios/default"

health:
  port: 8080

metrics:
  enabled: true
  port: 9090

logging:
  level: "info"
  format: "json"
```

### Multi-server mode

Multiple ICAP servers run inside a single process, each with its own port and scenario directory.

```yaml
defaults:
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  max_connections: 15000

servers:
  server-a:
    port: 1344
    scenarios_dir: "./configs/scenarios/server-a"
  server-b:
    port: 1488
    scenarios_dir: "./configs/scenarios/server-b"

health:
  port: 8080

metrics:
  enabled: true
  port: 9090
```

See `configs/example.yaml` for a full annotated configuration reference.

---

## Scenarios (v2 Format)

Scenario files define how the mock server responds to incoming ICAP requests. Each file has an optional `defaults` block and a map of named scenarios.

```yaml
defaults:
  method: RESPMOD                 # required (here or per-scenario)
  endpoint: /scan                 # required (here or per-scenario)
  status: 204
  headers:
    x-service: "ICAP Mock"
    x-verdict: "CLEAN"

scenarios:
  # Match by exact ICAP header value
  threat-exact:
    when:
      X-Filename: "malware.exe"
    set:
      x-verdict: "DANGEROUS"
      x-virus-id: "TROJAN"
    delay: 200ms-800ms

  # Match by regex on an ICAP header
  threat-hash:
    when:
      X-Filename: "re:[a-f0-9]{64}"
    set:
      x-verdict: "DANGEROUS"
    delay: 1s-3s

  # Match on the encapsulated HTTP message (headers / URL / method).
  # Content-Type lives inside the wrapped HTTP request, not in ICAP headers,
  # so it goes under `when_http`, not `when`.
  threat-dosexec:
    when_http:
      headers:
        Content-Type: "re:(?i)application/x-dosexec"
      url: "re:(?i)\\.exe(\\?|$)"
    status: 200
    http_status: 403

  # Weighted responses (probabilistic)
  flaky-service:
    responses:
      - weight: 80
        status: 204
        set:
          x-verdict: "CLEAN"
      - weight: 20
        status: 500
    delay: 100ms-300ms

  # Fallback (no `when` / `when_http` = always matches)
  default-response:
    set:
      x-verdict: "UNKNOWN"
    delay: 50ms-150ms
```

Scenarios are evaluated in priority order (file order by default); the first matching scenario
wins. `when:` matches ICAP-envelope headers, `when_http:` matches the encapsulated HTTP message
(its `headers`, `url`, and `method`) — combine them freely with AND semantics. A scenario without
a `when`/`when_http` block acts as a catch-all.

### Response templates, branches, path captures

For larger configs, pull reusable responses into a library and reference them by name. Branches
let one scenario dispatch to different responses by condition; path captures pull values out of
the endpoint and make them available inside response fields as `${name}` substitutions.

```yaml
defaults:
  method: [REQMOD, RESPMOD]
  endpoint: [/scan, /env/{env}/scan]

  # Named, reusable responses.
  response_templates:
    clean:
      status: 204
    blocked:                                   # synthesized HTTP block page
      status: 200                              # ICAP status
      http_status: 403                         # wrapped HTTP status
      http_set:                                # wrapped HTTP headers
        Content-Type: "text/html"
      http_body: "<html>blocked in ${env}</html>"   # wrapped HTTP body; ${env} from matched endpoint
    flaky:                                     # weighted template
      - { weight: 70, use: blocked }
      - { weight: 25, use: clean }
      - { weight: 5,  status: 500 }

  use: clean                                   # file-wide fallback (no scenario → 204)

scenarios:
  dispatch:
    branches:
      - when_http:
          headers: { Content-Type: "re:(?i)application/x-dosexec" }
        use: flaky                             # weighted outcome
      - when_http:
          headers: { Content-Type: "re:(?i)message/rfc822" }
        use: blocked
      - use: clean                             # branch-level catch-all
```

Mechanics:

- **`response_templates:`** defines inline or weighted responses that can be reused.
- **`use: <name>`** references a template at scenario, branch, or weighted-variant level.
- **`defaults.use:`** is the file-wide fallback applied when no scenario matched.
- **`set:` / `body:`** set the ICAP envelope headers and body. **`http_set:` / `http_body:` / `http_body_file:`** set the encapsulated HTTP response (what the origin client actually receives). `Content-Length` on the wrapped response is recomputed automatically from the body size unless you declare it explicitly in `http_set:` (use `"auto"` to force recompute).
- **`branches:`** holds several `when` / `when_http` → response pairs inside one scenario; first match wins. If none match, the scenario is skipped and the registry moves to the next scenario.
- **`endpoint:`** accepts a scalar or a list; each entry may include `{name}` captures that become regex groups in the path. Captured values are surfaced as `${name}` in `body`, `set`, and `http_headers`; use `$${` for a literal.
- **`method:`** accepts a scalar or a list, allowing one scenario to serve REQMOD and RESPMOD on the same port without duplication.

---

## CLI

```bash
# Start the server with a config file
icap-mock server --config configs/my-config.yaml

# Start with hot-reload enabled
icap-mock server --config configs/my-config.yaml --watch

# Replay recorded requests against a running server
icap-mock replay --input data/requests/ --target icap://localhost:1344/scan

# Launch the interactive terminal UI
icap-mock tui --config configs/my-config.yaml

# Validate a config or scenario file
icap-mock validate --config configs/my-config.yaml

# Test a scenario match against a sample request
icap-mock matchtest --scenario configs/scenarios/default/default.yaml
```

---

## Monitoring

Prometheus metrics are served at `http://localhost:9090/metrics` by default.

Available metrics include:

- `icap_requests_total` — total request count by method and endpoint
- `icap_request_duration_seconds` — request latency histogram
- `icap_active_connections` — current open connections per server
- `icap_rate_limited_total` — requests rejected by the rate limiter

A pre-built **Grafana dashboard** is available in `monitoring/grafana/dashboards/`. Start the full monitoring stack with:

```bash
docker-compose --profile monitoring up -d
```

Grafana will be available at `http://localhost:3000` (default credentials: `admin` / `admin`).

---

## Project Structure

```
icap-mock/
├── cmd/icap-mock/        # CLI entry point and subcommands
├── internal/
│   ├── config/           # Config loading and validation
│   ├── server/           # ICAP protocol server and connection handling
│   ├── handler/          # REQMOD, RESPMOD, OPTIONS handlers
│   ├── processor/        # Mock, echo, chaos, and JavaScript processors
│   ├── storage/          # Scenario registry (v1 legacy + v2 current)
│   ├── router/           # Request routing
│   ├── middleware/        # Rate limiter, body size limit, request logger
│   ├── metrics/          # Prometheus metrics and collector
│   ├── ratelimit/        # Sharded token-bucket implementation
│   ├── replay/           # Request replay engine
│   ├── health/           # /health and /ready HTTP handlers
│   ├── tui/              # Bubbletea terminal UI
│   └── circuitbreaker/   # Circuit breaker
├── pkg/
│   ├── icap/             # ICAP protocol types (request, response, headers)
│   ├── plugin/           # Plugin interface
│   └── pool/             # Buffer pools
├── configs/              # Example server and scenario configs
├── monitoring/           # Prometheus config and Grafana dashboards
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

---

## License

MIT — see [LICENSE](LICENSE) for details.
