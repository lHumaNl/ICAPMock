# icap-mock

> A production-ready ICAP mock server for load testing and integration testing of systems that communicate over the ICAP protocol (RFC 3507).

![Go version](https://img.shields.io/badge/go-1.25-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Build](https://img.shields.io/badge/build-passing-brightgreen)

---

## Features

- **Multi-server mode** — run multiple independent ICAP servers in a single process, each on its own port with its own scenario set
- **Scenario engine v2** — YAML-based scenario files with `defaults`, `when` (header matching), `set` (header overrides), and `delay` ranges
- **Weighted responses** — probabilistic response selection within a scenario
- **Regex header matching** — match incoming request headers against regular expressions (`re:` prefix)
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
  method: RESPMOD
  endpoint: /scan
  status: 204
  headers:
    x-service: "ICAP Mock"
    x-verdict: "CLEAN"

scenarios:
  # Match by exact header value
  threat-exact:
    when:
      X-Filename: "malware.exe"
    set:
      x-verdict: "DANGEROUS"
      x-virus-id: "TROJAN"
    delay: 200ms-800ms

  # Match by regex
  threat-hash:
    when:
      X-Filename: "re:[a-f0-9]{64}"
    set:
      x-verdict: "DANGEROUS"
    delay: 1s-3s

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

  # Fallback (no `when` = always matches)
  default-response:
    set:
      x-verdict: "UNKNOWN"
    delay: 50ms-150ms
```

Scenarios are evaluated top-to-bottom; the first matching scenario wins. A scenario without a `when` block acts as a catch-all default.

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
