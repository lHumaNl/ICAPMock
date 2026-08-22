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
- **Body streaming controls** — stream `request_http_body` / `response_http_body`, compose `stream.parts`, select multipart fields/files, and choose complete-vs-FIN endings
- **Response templates** — reusable named responses in `defaults.response_templates`, referenced via `use: <name>` from scenarios, branches, or weighted variants; `defaults.use:` acts as a file-wide fallback
- **Branches** — `branches:` list inside a scenario for OR-style dispatch with per-branch response (inline, `use:`, or weighted); first match wins; falls through to the next scenario if none match
- **Path captures** — endpoints like `/env/{id}/status` extract `{id}` from the URI; captured values are available as `${id}` in body/set/http_headers
- **Multi-method / multi-endpoint per port** — `method:` and `endpoint:` accept a scalar or a list; a single ICAP listener serves them all
- **Exact method-to-endpoint routes** — `routes:` binds REQMOD/RESPMOD to distinct endpoint sets without a Cartesian product
- **Flexible header matching** — exact values, regular expressions (`re:` prefix), generic
  contains-any lists, and fast media-type sets for `Content-Type`
- **Prometheus metrics** — expose request counts, latencies, and error rates at `/metrics`
- **Health checks** — HTTP `/health` and `/ready` endpoints for readiness probing
- **Hot-reload** — scenario files are watched and reloaded without restarting the server

---

## Quick Start

### Build from Source

Requires Go 1.25+.

```bash
git clone https://github.com/icap-mock/icap-mock.git
cd icap-mock
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
docker-compose up -d
```

---

## Configuration

The server supports two modes, selected by the top-level structure of the YAML config file.

### Single-server mode

```yaml
server:
  host: "0.0.0.0"
  port: 1344

mock:
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

See `configs/example.yaml` for a full annotated configuration reference. Configuration precedence
is: built-in defaults, then the YAML file, then `ICAP_` environment variables, then explicit CLI
flags.

### Management API and client identity

The health HTTP server can also expose authenticated management endpoints for live reloads:

- `POST /api/v1/scenarios/reload`
- `POST /api/v1/config/reload-current`
- `POST /api/v1/config/load` with JSON body `{"path":"/absolute/or/relative/config.yaml"}`

Enable them with `management.enabled: true`. If you do not set `management.token` or
`management.token_env`, the server still starts but logs a warning because the management API is
unauthenticated.

For client identity, the server uses the peer TCP remote address. `X-Client-IP` can still be used in
scenario header matching, but it is not trusted as `Request.ClientIP`.

---

## Scenarios (v2 Format)

Scenario files define how the mock server responds to incoming ICAP requests. Each file has an optional `defaults` block and a map of named scenarios.

```yaml
defaults:
  routes:                         # alternatively use legacy method + endpoint
    REQMOD: /av/reqmod
    RESPMOD: [/av/respmod, /av/scanfile]
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
      X-Filename: "re:^[a-f0-9]{64}$"
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
      - weight: 80.000
        status: 204
        set:
          x-verdict: "CLEAN"
      - weight: 20.000
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
a `when`/`when_http` block acts as a catch-all for its configured method and endpoint. In v2 files,
omitting `priority` preserves file order; use a negative explicit priority for a final fallback.

If every response variant omits `weight`, selection is uniform. Otherwise all variants must
define percentages rounded half-up to three decimals. Every value must be greater than `0.000`
and no greater than `100.000`, and the list must total exactly `100.000` after rounding. Mixing
weighted and unweighted variants is invalid.

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
      - { weight: 70.000, use: blocked }
      - { weight: 25.000, use: clean }
      - { weight: 5.000,  status: 500 }

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
- **`block:`** optionally overrides whether scenario metrics use `outcome="blocked"`. If omitted, the outcome is inferred from the selected concrete response: ICAP errors become `outcome="error"`; wrapped HTTP 4xx/5xx status, partial stream endings (`stream.end.mode: fin` / `term`, legacy `stream.finish.mode: fin`), or weighted FIN with `fin_percent > 0` become `outcome="blocked"`.
- **`branches:`** holds several `when` / `when_http` → response pairs inside one scenario; first match wins. If none match, the scenario is skipped and the registry moves to the next scenario.
- **`endpoint:`** accepts a scalar or a list; each entry may include `{name}` captures that become regex groups in the path. Captured values are surfaced as `${name}` in `body`, `set`, and `http_headers`; use `$${` for a literal.
- **`method:`** accepts a scalar or a list, allowing one scenario to serve REQMOD and RESPMOD on the same port without duplication.

### Streaming, multipart selectors, and finish modes

Streaming scenarios can reuse the encapsulated HTTP body directly instead of buffering a separate
inline response. Public examples live in `configs/scenarios/example/example.yaml`.

```yaml
scenarios:
  reqmod-body-stream:
    endpoint: /stream/request-http-body
    status: 200
    stream:
      source:
        from: request_http_body
      throttle:
        target_chunk_size: 16

  multipart-upload-stream:
    endpoint: /stream/multipart-upload
    status: 200
    stream:
      source:
        from: request_http_body
      multipart:
        fields: [comment]
        files:
          filename: ".*\\.(txt|bin)$"
      fallback:
        body: "no matching multipart parts selected\n"

  partial-term-stream:
    endpoint: /stream/partial-term
    status: 200
    stream:
      source: { from: body, body: "preview-approved" }
      send:
        percent: "40%"
        duration: 250ms
      throttle:
        target_chunk_size: 4
      end:
        mode: term
```

Notes:

- `request_http_body` is valid for REQMOD scenarios; `response_http_body` is valid for RESPMOD.
- `adapted_http_body` works with explicit REQMOD and/or RESPMOD methods, selecting the request body
  for REQMOD and response body for RESPMOD so one response template can serve both methods.
- `stream.parts` concatenates multiple sources in order.
- `multipart.fields` matches part names exactly; `multipart.files.filename` uses regex patterns.
- `fallback.raw_file` is for non-multipart raw source bodies only. For multipart selector misses,
  use `multipart.allow_empty: true` or an explicit safe fallback such as `fallback.body`,
  `fallback.body_file`, or a supported `fallback.from` source.
- Preferred stream controls are `send`, `throttle`, and `end`. `end.mode: complete` sends the full
  body and permits `send.duration` and/or `throttle` pacing. `end.mode: fin` and `end.mode: term`
  require a partial `send.percent` (`1..99`) plus `send.duration` or `throttle.every`, and never send
  more than that selected percentage. `fin` closes without the terminating chunk; `term` sends the
  terminating chunk after the selected partial body. Legacy `chunks` / `duration` / `finish` remain
  supported for existing files, but cannot be mixed with `send` / `throttle` / `end`.
- `end.mode: fin` keeps ICAP response headers normal: ICAPMock does not add `Connection: close`;
  it writes the partial chunked body, omits the terminating chunk, and then closes the TCP stream.
- `throttle.target_chunk_size` is an adaptive preferred size, while `target_chunks` is a non-strict
  preferred count; they are mutually exclusive. `throttle.every` is the minimum interval between
  non-empty body chunks and can be combined with `send.duration`.
- Use response-level `delay` to postpone the whole response. A duration range is selected once per
  stream, and terminal scenario latency includes response delivery, pacing, flush, and FIN close.

---

## CLI

```bash
# Start the server with a config file
icap-mock server --config configs/my-config.yaml

# Validate a server config without starting listeners
icap-mock server --config configs/my-config.yaml --validate

# Validate all v1/v2 scenario YAML files in a directory
icap-mock validate-scenarios --dir ./configs/scenarios

# Test a scenario match against a sample request
icap-mock match-test --scenarios ./configs/scenarios/example --path /example \
  --method REQMOD --header X-Filename:malware.exe --verbose
```

---

## Monitoring

Prometheus metrics are served at `http://localhost:9090/metrics` by default.

Available metrics include:

- `icap_requests_total{content_type,method,outcome,server,response,scenario}` — canonical ICAP request count, recorded once at the request's terminal outcome; successful `allowed`/`blocked` outcomes are recorded only after response delivery, while response write/flush failures use `outcome="error"`; `content_type` is normalized from encapsulated HTTP headers and truncated to 120 characters, and matched requests include scenario/response labels
- `icap_request_errors_total{server,method,stage,error_type,scenario,response}` — bounded request error count for context cancellation, body receive, routing, scenario match, processor response/build, and response write failures; raw error text is logged but never used as a metric label
- `icap_errors_total{server,type}` — aggregate ICAP error count, including routing failures such as `route_not_found` and response delivery failures such as `response_write_failed`
- `icap_active_connections{server}` — current open connections per configured server
- `icap_requests_in_flight{server,method}` — requests that entered routing and have not yet reached terminal response delivery or terminal failure; includes ordinary writes, stream pacing, final flush, and FIN close, but excludes keep-alive waiting
- `icap_requests_processing_in_flight{server,method}` — REQMOD/RESPMOD requests currently executing the shared handler and processor through response preparation; excludes response delivery
- `icap_scenario_response_duration_seconds{content_type,method,outcome,server,response,scenario}` — server-side elapsed time from timed scenario handling through terminal delivery; includes response delay, stream pacing, writes, final flush, and FIN close, but excludes request upload and keep-alive wait
- `icap_scenario_processing_duration_seconds{content_type,method,outcome,server,response,scenario}` — matched scenario processor wall time through response construction; this summary intentionally exposes only `_sum` and `_count`, with no buckets or quantiles
- `icap_streaming_active{server}` — responses currently executing streaming body delivery or pacing per configured server
- `icap_scenarios_loaded{server}` — currently loaded scenario count
- `icap_api_requests_total{server,route,method,status_code}` — management API calls with bounded route labels
- `icap_api_errors_total{server,route,method,status_code,error_type}` — failed management API calls

A pre-built **Grafana dashboard** is available at `monitoring/grafana/ICAP Mock.json`. Import that
file into Grafana and select the Prometheus data source that scrapes ICAP Mock. The dashboard
includes request error rate/table panels and Go/process runtime utilization panels for GC, CPU,
heap/RSS memory, goroutines, Go threads, file descriptors, network receive/transmit, and uptime.

`icap_scenario_response_duration_seconds` uses classic Prometheus buckets. The finite bucket
layout starts with `0.001, 0.01, 0.05`, then `0.1, 0.2, ..., 1.0`, then `1.25, 1.5, 1.75, 2.0`, then `2.5, 3.0, ..., 5.0`,
then `6, 7, 8, 9, 10`, then `15, 20, ..., 60`, then `70, 80, ..., 120` seconds,
plus the implicit `+Inf` bucket.

`icap_scenario_processing_duration_seconds` is a summary without configured quantiles. Use its
`_sum` and `_count` series to calculate average processor time without the additional series created
by histogram buckets or client-side summary quantiles.

For a slow stream, `icap_requests_in_flight` and `icap_streaming_active` remain positive until
terminal delivery, while `icap_requests_processing_in_flight` returns to zero as soon as the prepared
response is handed back to the server. `icap_active_connections` may remain positive afterward while
the connection waits for another keep-alive request.

Scrape it with standard Prometheus-compatible text or OpenMetrics settings:

```yaml
scrape_configs:
  - job_name: icap-mock
    static_configs:
      - targets: ["icap-mock:9090"]
```

Query scenario latency with `_bucket` and `le`, for example:

```promql
histogram_quantile(
  0.95,
  sum by (le, content_type, method, outcome, server, response, scenario) (
    rate(icap_scenario_response_duration_seconds_bucket[5m])
  )
)
```

---

## Project Structure

```
icap-mock/
├── cmd/icap-mock/        # CLI entry point and subcommands
├── internal/
│   ├── config/           # Config loading and validation
│   ├── server/           # ICAP protocol server and connection handling
│   ├── handler/          # REQMOD, RESPMOD, OPTIONS handlers
│   ├── processor/        # Mock and echo processors
│   ├── storage/          # Scenario registry (v1 legacy + v2 current)
│   ├── router/           # Request routing
│   ├── middleware/        # Storage, panic recovery, body size limit, request logger
│   ├── metrics/          # Prometheus metrics and collector
│   ├── health/           # /health and /ready HTTP handlers
│   └── management/       # Runtime management API
├── pkg/
│   ├── icap/             # ICAP protocol types (request, response, headers)
│   └── pool/             # Buffer pools
├── configs/              # Example server and scenario configs
├── monitoring/           # Importable Grafana dashboard
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

---

## Release hygiene

Do not include `*.pcap` or `*.pcapng` captures in release artifacts or Docker build contexts; keep captures as local verification references only.

---

## License

MIT — see [LICENSE](LICENSE) for details.
