# ICAP Mock — Configuration Reference

## Overview

The `configs/` directory contains two kinds of configuration files:

- **Main config** (`*.yaml` at the top level) — controls server settings: ports, timeouts,
  logging, metrics, hot reload, rate limits, etc.
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

### `scenarios` map

Each key is a scenario name. A scenario activates when all conditions in its `when:` / `when_http:`
blocks match the incoming request. If multiple scenarios match, the one with the highest `priority:`
wins (default priority is `0`; higher numbers win; file order is used as a tiebreaker).

`method` and `endpoint` are required — either in `defaults:` or on the scenario itself.
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
    priority: 10

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
  - `headers:` — map of HTTP header → value (exact or `re:`-prefixed regex).
  - `url:` — exact string or `re:` regex matched against the URI of the wrapped HTTP request
    (useful when the filename only appears in the URL, e.g. `http://host/path/file.exe`).
  - `method:` — HTTP method of the wrapped request (`GET`, `POST`, …).

A common foot-gun is putting `Content-Type` under `when:` — Content-Type is an HTTP header, not
an ICAP one, so the ICAP envelope does not contain it and the scenario never matches. Use
`when_http.headers` instead.

```yaml
block-pe-files:
  when_http:
    headers:
      Content-Type: "re:(?i)application/x-dosexec"
  status: 200
  http_status: 403

block-by-url:
  when_http:
    url: "re:(?i)/[^/]+\\.(exe|dll|scr)(\\?|$)"
  status: 200
  http_status: 403

internal-client-only:
  when:
    X-Client-IP: "re:^10\\."
  when_http:
    method: POST
  status: 204
```

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
      - weight: 70
        use: blocked
      - weight: 25
        use: clean
      - weight: 5
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
          X-Client-IP: "re:^10\\."
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

### Weighted responses

Use `responses:` to return different answers with configurable probability. Weights are relative
integers (they do not need to sum to 100). When `responses:` is present, top-level `status`,
`set`, `body`, and `delay` are ignored — each variant declares its own values.

```yaml
scan-file:
  when:
    X-Client-ID: "re:.+"
  responses:
    - weight: 80
      status: 204
      set:
        X-Threat-Level: "CLEAN"
      delay: 50ms-150ms

    - weight: 15
      status: 200
      http_status: 200
      set:
        X-Threat-Level: "SUSPICIOUS"
      http_set:
        Content-Type: "text/plain"
      http_body: "File flagged but allowed."
      delay: 200ms-600ms

    - weight: 5
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
    X-Filename: "Worm.BAT.Autorun.u"
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
| `ICAP_SCENARIOS_DIR` | `mock.scenarios_dir` |
| `ICAP_LOG_LEVEL` | `logging.level` |
| `ICAP_LOG_FORMAT` | `logging.format` |
| `ICAP_API_TOKEN` | `health.api_token` |

CLI flags take priority over environment variables, which take priority over the YAML file.

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
