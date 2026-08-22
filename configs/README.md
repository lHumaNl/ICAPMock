# ICAP Mock — Configuration Reference

## Overview

The `configs/` directory contains two kinds of configuration files:

- **Main config** (`*.yaml` at the top level) — controls server settings: ports, timeouts,
  logging, metrics, hot reload, and management endpoints.
- **Scenario configs** (`scenarios/<name>/*.yaml`) — define how the mock responds to requests:
  which headers to match, what status codes and bodies to return, how long to delay, etc.

---

## Directory structure

```
configs/
  example.yaml                     # full annotated main config reference
  scenarios/
    example/
      example.yaml                 # full annotated scenario reference
```

---

## Main config

### Single-server mode

One ICAP listener. Use the `server:` + `mock:` top-level keys:

```yaml
server:
  host: "0.0.0.0"
  port: 1344

mock:
  scenarios_dir: "./configs/scenarios/example"
  hot_reload:
    enabled: true

logging:
  level: "info"
  format: "json"
```

### Multi-server mode

Multiple ICAP listeners in one process. Replace `server:` + `mock:` with `defaults:` + `servers:`.
Each entry under `servers:` inherits from `defaults:` and can override any field:

```yaml
defaults:
  host: "0.0.0.0"
  read_timeout: 30s
  max_connections: 15000

servers:
  scanner-a:
    port: 1344
    scenarios_dir: "./configs/scenarios/scanner-a"
    service_id: "scanner-a-icap"

  scanner-b:
    port: 1345
    scenarios_dir: "./configs/scenarios/scanner-b"
    service_id: "scanner-b-icap"
```

See `example.yaml` for the complete list of available fields with defaults and descriptions.

### Request metrics labels

Prometheus metrics are enabled by default. The canonical request counter is
`icap_requests_total{content_type,method,outcome,server,response,scenario}`. It is recorded once at the request's
terminal outcome. Successful `allowed` or `blocked` outcomes are recorded only after response delivery; response
write or flush failures use `outcome="error"`. The `content_type` label is parsed from
the encapsulated HTTP `Content-Type` header, lowercased, stripped of parameters, truncated to 120
characters, and bounded to known or safely admitted media types. REQMOD uses the HTTP request
header, RESPMOD uses the HTTP response header, and OPTIONS or missing HTTP headers use
`content_type="none"`.

Outcome values are `allowed`, `blocked`, and `error`.

Request error observability uses
`icap_request_errors_total{server,method,stage,error_type,scenario,response}`. Stages are bounded to
`context`, `body_receive`, `routing`, `processor_match`, `processor_response`, `processor_build`, and
`write_response`; error types are likewise bounded (for example `context_canceled`,
`deadline_exceeded`, `route_not_found`, `no_scenario_match`, `response_build_failed`, and
`response_write_failed`). Raw error
descriptions are emitted only in structured ERROR logs.
The aggregate `icap_errors_total{server,type}` counter also includes routing failures such as
`route_not_found` and response delivery failures such as `response_write_failed`.

Request lifecycle gauges use bounded `server` and `method` labels:

- `icap_requests_in_flight{server,method}` remains active from routing through terminal response
  delivery, including ordinary writes, stream pacing, final flush, and FIN close. Keep-alive waiting
  is excluded.
- `icap_requests_processing_in_flight{server,method}` preserves the shared REQMOD/RESPMOD handler
  boundary and returns to zero when response preparation finishes, before response delivery.
- `icap_streaming_active{server}` is the subset currently executing streaming body delivery or
  pacing.

Scenario response latency uses the classic histogram
`icap_scenario_response_duration_seconds{content_type,method,outcome,server,response,scenario}` with
finite buckets at 0.001, 0.01, and 0.05 seconds, then 0.1-second intervals through 1 second, 0.25-second intervals through 2 seconds,
0.5-second intervals through 5 seconds, 1-second intervals through 10 seconds, 5-second intervals
through 60 seconds, and 10-second intervals through 120 seconds. Query quantiles from
`icap_scenario_response_duration_seconds_bucket` with `le`.
Request size histograms also remain classic histograms.

Matched scenario processor time is reported separately as
`icap_scenario_processing_duration_seconds{content_type,method,outcome,server,response,scenario}`.
It covers processor execution through response construction and exposes only `_sum` and `_count`;
it has no histogram buckets or summary quantiles.

### Body-pattern safety limits

`mock.matching.body_pattern_limit` and `mock.matching.body_pattern_limit_action` protect
body-regex matching from unbounded reads. They apply to legacy scenarios that use
`match.body_pattern`.

```yaml
mock:
  matching:
    body_pattern_limit: 10mb
    body_pattern_limit_action: no_match   # or: error
```

- `no_match` — oversized bodies simply do not satisfy `body_pattern`; other scenarios may still match.
- `error` — matching stops with a controlled error instead of silently skipping the scenario.
- `unlimited` is allowed, but the effective limit is still capped by `server.max_body_size` when that
  server-side limit is finite.

### Management API and reloads

The health HTTP server can expose runtime management endpoints when `management.enabled: true`:

```yaml
management:
  enabled: true
  scenario_reload_enabled: true
  config_reload_enabled: true
  token_env: ICAP_MANAGEMENT_TOKEN
```

Available routes:

- `POST /api/v1/scenarios/reload`
- `POST /api/v1/config/reload-current`
- `POST /api/v1/config/load` with `{"path":"configs/example.yaml"}`

If no token is configured, the server logs a warning because the management API is left open.

Client identity is derived from the peer TCP remote address. Headers such as `X-Client-IP` remain
available for scenario matching but do not set `Request.ClientIP`.

---

## Scenario config (v2 format)

### `defaults` block

Fields declared under `defaults:` are inherited by every scenario in the file. Individual
scenarios can override any field.

```yaml
defaults:
  method: RESPMOD
  endpoint: /scan-file
  status: 204
  headers:
    Server: "ICAP Mock"
    ISTag: '"mock-2026"'
```

For exact method-to-endpoint pairing, use `routes` instead of independent `method` and `endpoint`
lists:

```yaml
defaults:
  routes:
    REQMOD: /av/reqmod
    RESPMOD: [/av/respmod, /av/scanfile]
```

This declares exactly three pairs; it does not allow `REQMOD /av/respmod` or
`RESPMOD /av/reqmod`. `routes` is also valid inside an individual scenario. Scenario-level
`routes` replaces `defaults.routes` completely; it is never merged.

When `defaults.routes` is present, scenario routing overrides resolve as follows:

| Scenario routing fields | Effective routing |
|---|---|
| none | inherit all `defaults.routes` |
| `routes` | replace the complete default route map |
| only `method` | select those methods and their endpoints from `defaults.routes` |
| only `endpoint` | replace endpoints for every method in `defaults.routes` |
| `method` + `endpoint` | explicit legacy Cartesian product |

Do not combine `routes` with `method` or `endpoint` on the same YAML level. `routes` accepts only
`REQMOD` and `RESPMOD`; OPTIONS continues to use the legacy configuration because OPTIONS
responses are produced by the capability handler rather than scenario processing.

### `scenarios` map

Each key is a scenario name. A scenario activates when all conditions in its `when:` / `when_http:`
blocks match the incoming request. If multiple scenarios match, the one with the highest `priority:`
wins. When `priority` is omitted, v2 assigns descending priorities from `1000` to preserve file
order. A value of `0` is also treated as omitted. When mixing explicit and implicit priorities,
compare explicit values with that generated range; a negative value is suitable for a final
catch-all.

Either `routes`, or the resolved legacy `method` and `endpoint`, is required in `defaults:` or on
the scenario itself.
Loading fails with a clear error otherwise (avoids scenarios silently matching every request
because of a missing filter).

`method` accepts either a single string or a list:

```yaml
method: REQMOD                   # single method
method: [REQMOD, RESPMOD]        # scenario applies to both
```

With a list, the scenario matches a request whose method is any of the listed values. Allowed
values are `REQMOD`, `RESPMOD`, `OPTIONS`; anything else fails validation at load time.

One ICAP listener serves all methods on a single port. You can have REQMOD, RESPMOD, and OPTIONS
scenarios side-by-side — with the same or different endpoints. Two scenarios sharing the same
`endpoint` but declaring different methods are both registered and dispatched by the request's
ICAP method.

```yaml
scenarios:
  block-malware:
    when:
      X-Filename: "malware.exe"
    status: 200
    http_status: 403
    set:                                # ICAP envelope headers
      X-Threat-Level: "DANGEROUS"
    http_set:                           # wrapped HTTP headers
      Content-Type: "text/html"
    http_body: "<html><body><h1>Blocked</h1></body></html>"
    delay: 300ms
    priority: 1100

  catch-all:
    status: 204
    set:
      X-Threat-Level: "CLEAN"
    delay: 10ms-50ms
    priority: -10
```

### Match types

| Pattern | Behaviour |
|---------|-----------|
| `"invoice.pdf"` | Exact string match |
| `"re:(?i)\.exe$"` | Go regex (case-insensitive executable extension) |

A scenario with no `when:` / `when_http:` block matches every request — useful as a catch-all.
Within a block all conditions must match simultaneously (logical AND); across blocks the logic is
also AND.

### Matching ICAP headers vs encapsulated HTTP (`when:` vs `when_http:`)

ICAP requests carry two separate sets of headers: the ICAP envelope (Host, X-Client-IP,
X-Server-IP, Allow, Encapsulated, any custom ICAP headers the client sends) and the headers of the
HTTP request/response encapsulated inside it (Content-Type, Content-Length, the URL, and so on).
The two are matched by different blocks:

- `when:` — matches **ICAP** headers.
- `when_http:` — matches the **encapsulated HTTP** message. Fields:
  - `headers:` — map of HTTP header → scalar or list. A scalar is exact, or a regex when prefixed
    with `re:`. Generic-header lists are case-insensitive contains-any matchers. `Content-Type`
    lists use the media-type rules below.
  - `url:` — exact string or `re:` regex matched against the URI of the wrapped HTTP request
    (useful when the filename only appears in the URL, e.g. `http://host/path/file.exe`).
  - `method:` — HTTP method of the wrapped request (`GET`, `POST`, …).

Both `when:` and `when_http.headers:` accept scalar and list values. The same list syntax therefore
works for custom ICAP-envelope headers as well as encapsulated HTTP headers.

A common foot-gun is putting `Content-Type` under `when:` — Content-Type is an HTTP header, not
an ICAP one, so the ICAP envelope does not contain it and the scenario never matches. Use
`when_http.headers` instead.

```yaml
block-pe-files:
  when_http:
    headers:
      Content-Type:
        - application/x-dosexec
        - application/x-executable
  status: 200
  http_status: 403

block-by-url:
  when_http:
    url: "re:(?i)/[^/]+\\.(exe|dll|scr)(\\?|$)"
  status: 200
  http_status: 403

internal-client-only:
  when:
    X-Client-IP: "re:^192\\.0\\.2\\."
  when_http:
    method: POST
  status: 204
```

For a `Content-Type` list item without `;`, ICAPMock compares the normalized media type through a
prebuilt set and ignores request parameters. Thus `application/pdf` matches
`Application/PDF; charset=binary`, but not `application/pdfx`. List order does not affect this fast
path. If a configured item contains `;`, it is parsed as a parameter-aware rule: media type and all
listed parameters must match, parameter order is irrelevant, and extra request parameters are
allowed. Parameter names and `charset` values are case-insensitive; other parameter values remain
case-sensitive. List items are literals (`re:` has no special meaning), and invalid media types,
empty lists, and empty/non-string items are rejected.

Scenarios that use `when_http:` only match requests that actually carry an encapsulated HTTP
message — OPTIONS requests fall through to the next scenario.

### Delay

- **Static**: `delay: 500ms` — always waits exactly 500 ms.
- **Range**: `delay: 300ms-1500ms` — waits a random duration in [300 ms, 1500 ms].

Any Go duration unit is accepted: `ms`, `s`, `m`, `h`.

### Response shape: ICAP envelope vs encapsulated HTTP

An ICAP response carries both an envelope (status, headers, optional body) and
an encapsulated HTTP message (the request/response being modified). Scenarios
can shape both sides independently:

| YAML field        | Writes to                                                       |
|-------------------|-----------------------------------------------------------------|
| `status:`         | ICAP status code                                                |
| `set:`            | ICAP response headers (envelope)                                |
| `body:`/`body_file:` | ICAP body (rarely needed; pass-through of modified chunks)   |
| `http_status:`    | Encapsulated HTTP status code                                   |
| `http_set:`       | Encapsulated HTTP headers                                       |
| `http_body:`      | Encapsulated HTTP body (inline string)                          |
| `http_body_file:` | Encapsulated HTTP body (loaded from a file on disk)             |
| `block:`          | Optional metrics override for allow/block outcome               |

The typical "block page" response uses the HTTP side:

```yaml
blocked-page:
  status: 200                      # ICAP: we're returning a modified response
  http_status: 403                 # wrapped HTTP status the client sees
  http_set:
    Content-Type: "text/html; charset=utf-8"
    X-Block-Reason: "AV policy"
  http_body: |
    <!DOCTYPE html>
    <html><body><h1>Blocked</h1></body></html>
```

On the wire the mock emits a valid ICAP response with `Encapsulated:
res-hdr=N, res-body=M`, the HTTP status line + headers, and the body in
chunked transfer encoding per RFC 3507. `Content-Length` on the wrapped
response is recomputed to match `http_body` automatically; to pin a specific
value (e.g. deliberately lying to the client), declare it in `http_set:`.
Use `Content-Length: "auto"` to force recomputation even when the header is
set, e.g. inherited from a template.

Placeholders (`${name}`) from endpoint captures (see below) also expand inside
`http_body`, `http_set` values, `body`, and `set` values.

`block:` can be set to `true` or `false` on response templates, inline scenario
responses, branches, and weighted variants. When omitted, the metrics `outcome`
label is inferred from the selected concrete response after template, branch,
and weighted resolution: ICAP errors use `outcome="error"`; wrapped HTTP 4xx/5xx,
partial stream endings (`stream.end.mode: fin` / `term`, legacy `stream.finish.mode: fin`) or weighted FIN with `fin_percent > 0` use `outcome="blocked"`; all other responses use `outcome="allowed"`.

### Response templates + `use:` references

Define reusable named responses in `defaults.response_templates` and reference them via
`use: <name>` at scenario, branch, or weighted-variant level. A template is either an inline
response (map) or a weighted list. Templates are flat — they cannot themselves `use:` another
template; cycles are impossible by construction. `defaults.use:` picks a template as the
**file-wide fallback**: returned when no scenario matched at all (otherwise ICAP 404).

```yaml
defaults:
  response_templates:
    clean:
      status: 204
    blocked:
      status: 200
      http_status: 403
      http_set: { Content-Type: "text/html" }
      http_body: "<html>blocked</html>"
    flaky-block:
      - weight: 70.000
        use: blocked
      - weight: 25.000
        use: clean
      - weight: 5.000
        status: 500
  use: clean

scenarios:
  s1:
    use: flaky-block        # scenario-level reference (weighted, sampled per-request)
  s2:
    use: blocked            # simple reference to an inline template
```

### Branches

Use `branches:` to put several condition → response pairs under a single scenario. Branches are
evaluated in file order; the first match wins. If no branch matches, the scenario is considered
non-matching and the registry moves on to the next scenario. `branches:` is **mutually exclusive**
with scenario-level response fields (`status`, `body`, `use`, `responses`, …) — for a fallback,
add an explicit catch-all branch at the end without any `when`/`when_http`.

```yaml
scenarios:
  scan-dispatch:
    # No scenario-level when/when_http → gate is method+endpoint only.
    branches:
      - when_http:
          headers: { Content-Type: "re:(?i)application/x-dosexec" }
        use: blocked
      - when:
          X-Client-IP: "re:^192\\.0\\.2\\."
        use: flaky-block      # branch may reference a weighted template
      - use: clean             # catch-all inside the scenario
```

### Endpoint list + path captures

`endpoint:` accepts either a single path (`/scan`) or a list (`["/v1/scan", "/v2/scan"]`). Any
entry may include `{name}` placeholders — compiled to a regex-named capture group `[^/]+`. On a
successful match the captured values are accessible via `${name}` substitutions inside `body`,
`set`, `http_body`, and `http_set` values. Use `$${…}` to embed a literal dollar-brace sequence.

```yaml
scenarios:
  env-status:
    endpoint: /env/{env}/items/{id}/status
    status: 200
    http_status: 200
    http_set:
      Content-Type: "application/json"
      X-Env-Id: "${id}"
    http_body: |
      {"env": "${env}", "id": "${id}", "status": "ok"}
```

A capture-style endpoint is registered with the router as a regex pattern, so captures work
transparently alongside plain paths declared in the same list.

### Streaming encapsulated bodies

`stream:` is available on inline responses, branches, and weighted variants. It streams bytes in
chunked ICAP output without forcing a separate `http_body` / `http_body_file` payload.

```yaml
scenarios:
  reqmod-stream:
    method: REQMOD
    endpoint: /stream/request
    status: 200
    stream:
      source:
        from: request_http_body
      throttle:
        target_chunk_size: 16

  respmod-stream:
    method: RESPMOD
    endpoint: /stream/response
    status: 200
    stream:
      source:
        from: response_http_body
      send:
        percent: "40%"
        duration: 250ms
      throttle:
        target_chunk_size: 16
      end:
        mode: fin
```

- `request_http_body` requires an explicit `REQMOD` method.
- `response_http_body` requires an explicit `RESPMOD` method.
- `adapted_http_body` supports explicit `REQMOD` and/or `RESPMOD` methods and selects the request
  body for REQMOD or response body for RESPMOD. It is useful in one shared response template.
- Preferred controls are `send`, `throttle`, and `end`. `end.mode: complete` sends the full body;
  `end.mode: fin` and `end.mode: term` require partial `send.percent` (`1..99`) plus
  `send.duration` or `throttle.every`. `fin` closes without a terminating chunk; `term` sends the
  terminating chunk after the selected partial body. Legacy `chunks` / `duration` / `finish` remain
  supported for existing files, but cannot be mixed with `send` / `throttle` / `end`.
- `end.mode: fin` does not add `Connection: close`; ICAPMock writes the partial chunked body,
  omits the terminating chunk, and then closes the TCP stream.
- `throttle.target_chunk_size` is an adaptive preferred size. Alternatively, `target_chunks` is a
  non-strict preferred count; the two hints are mutually exclusive and all plans are capped at 1000
  non-empty body chunks. `throttle.every` is a minimum chunk-start interval and can be combined with
  `send.duration`.
- Use response-level `delay` to postpone the whole response. Streaming duration ranges are selected
  once per response. `icap_scenario_response_duration_seconds` ends only after terminal delivery,
  including pacing, final flush, and FIN close, while request upload and keep-alive wait are excluded.
  `icap_scenario_processing_duration_seconds` ends when the processor has constructed the response,
  before body delivery begins.

### `stream.parts`

Use `parts:` to concatenate several sources in order. `stream.from` and `stream.parts` are mutually
exclusive.

```yaml
scenarios:
  composite-stream:
    method: RESPMOD
    endpoint: /stream/parts
    status: 200
    stream:
      parts:
        - from: response_http_body
        - body: "\n-- streamed by icap-mock --\n"
        - from: response_body
```

### Multipart selectors and safe fallback

Multipart selectors only work with `request_http_body`, `response_http_body`, or
`adapted_http_body` sources because they
inspect the encapsulated HTTP message body.

```yaml
scenarios:
  multipart-upload:
    method: REQMOD
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
```

- `multipart.fields` matches part names exactly.
- `multipart.files: true` selects all file parts.
- `multipart.files.filename` filters file parts by regex.
- `fallback.raw_file` is only for non-multipart raw source bodies. For multipart selector misses,
  use `multipart.allow_empty: true` or an explicit safe fallback such as `fallback.body`,
  `fallback.body_file`, or a supported `fallback.from` source.

### Legacy weighted complete-vs-FIN ending

```yaml
scenarios:
  weighted-finish:
    method: REQMOD
    endpoint: /stream/weighted-finish
    status: 200
    stream:
      source: { from: body, body: "preview-approved" }
      finish:
        mode: weighted
        complete_percent: 80
        fin_percent: 20
        fin:
          close: clean
```

For weighted mode, `complete_percent + fin_percent` must equal `100`. If `fin_percent` is non-zero,
`finish.fin` must also be configured.

### Weighted responses

Use `responses:` to return different answers with configurable probability. If every variant
omits `weight`, selection is uniform. Otherwise every variant must define a weight, each weight
is rounded to three decimals and must be greater than `0.000` and no greater than `100.000`, and
the list must total exactly `100.000` after rounding. Mixing weighted and unweighted variants is
invalid. Scenario/branch response fields are inherited as defaults by every variant.

Rounding uses the conventional half-up rule: for example, `1.2344` becomes `1.234`, while
`1.2345` becomes `1.235`. The minimum representable probability is `0.001%`.

```yaml
scan-file:
  when:
    X-Client-ID: "re:.+"
  responses:
    - weight: 80.000
      status: 204
      set:
        X-Threat-Level: "CLEAN"
      delay: 50ms-150ms

    - weight: 15.000
      status: 200
      http_status: 200
      set:
        X-Threat-Level: "SUSPICIOUS"
      http_set:
        Content-Type: "text/plain"
      http_body: "File flagged but allowed."
      delay: 200ms-600ms

    - weight: 5.000
      status: 200
      http_status: 403
      set:
        X-Threat-Level: "DANGEROUS"
      http_set:
        Content-Type: "text/html"
      http_body: "<html><body><h1>Blocked</h1></body></html>"
      delay: 300ms-1s
```

### `body` / `body_file` / `http_body` / `http_body_file`

- `body: |` / `body_file:` — ICAP-envelope body. Rarely needed; used when the
  mock is returning raw ICAP-level content instead of wrapping an HTTP message.
- `http_body: |` / `http_body_file:` — body of the encapsulated HTTP response.
  This is what the origin client actually receives (a block page, a JSON
  document, etc.). Served with proper `Encapsulated: res-body=N` and chunked
  transfer encoding.
- Relative `*_file:` paths resolve from the process working directory. Inline
  wins over file if both are set on the same level.
- `Content-Length` on the wrapped response is recomputed automatically to
  match the body. To pin a specific value, declare `Content-Length` in
  `http_set:`; use `Content-Length: "auto"` to force recomputation.

### Practical examples

**1. Block a specific filename:**
```yaml
block-known-malware:
  when:
    X-Filename: "synthetic-block-a.bin"
  status: 200
  http_status: 403
  set:
    X-Threat-Level: "DANGEROUS"
    X-Virus-ID: "TROJAN"
  delay: 500ms-1500ms
```

**2. Block all executables by extension (regex):**
```yaml
block-executables:
  when:
    X-Filename: "re:(?i)\.(exe|dll|bat|ps1)$"
  status: 200
  http_status: 403
  set:                                          # ICAP envelope
    X-Threat-Level: "SUSPICIOUS"
  http_set:                                     # wrapped HTTP
    Content-Type: "text/plain"
  http_body: "Executable file blocked by policy."
  delay: 100ms-500ms
  priority: 10
```

**3. Multiple conditions — combining ICAP and HTTP matchers (AND logic):**
```yaml
block-email-attachment:
  method: RESPMOD
  when:
    X-Service-Name: "re:(?i)mail-gateway"     # ICAP-level custom header
  when_http:
    headers:
      Content-Type: "re:(?i)message/rfc822"   # HTTP-level header
  status: 200
  http_status: 403
  set:
    X-Block-Reason: "Malicious email attachment"
  http_set:
    Content-Type: "text/plain"
  http_body: "Email attachment blocked."
  delay: 800ms-2s
  priority: 20
```

---

## Environment variable overrides

| Variable | Overrides |
|---|---|
| `ICAP_SERVER_HOST` | `server.host` |
| `ICAP_SERVER_PORT` | `server.port` |
| `ICAP_MOCK_SCENARIOS_DIR` | `mock.scenarios_dir` |
| `ICAP_LOGGING_LEVEL` | `logging.level` |
| `ICAP_LOGGING_FORMAT` | `logging.format` |
| `ICAP_API_TOKEN` | `health.api_token` |

The complete naming pattern is `ICAP_<SECTION>_<KEY>`. Precedence from lowest to highest is:
built-in defaults, YAML file, environment variables, explicit CLI flags.

---

## Hot reload

Scenario files can be reloaded without restarting the server. Enable in the main config:

```yaml
mock:
  hot_reload:
    enabled: true
    debounce: 1s        # wait this long after the last change before reloading
    watch_directory: true
```

When a scenario file is saved, the server detects the change and reloads the scenario registry
with zero downtime. No signal or restart is required.

For exact `routes`, live reload may change methods, conditions, and responses while preserving the
same endpoint declaration set. Adding, removing, or changing an endpoint is rejected and requires
a process restart so the ICAP router table cannot become stale.
