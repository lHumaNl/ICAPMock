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

Each key is a scenario name. A scenario activates when all conditions in its `when:` block match
the incoming request headers. If multiple scenarios match, the one with the highest `priority:`
wins (default priority is `0`; higher numbers win; file order is used as a tiebreaker).

```yaml
scenarios:
  block-malware:
    when:
      X-Filename: "malware.exe"
    status: 200
    http_status: 403
    set:
      Content-Type: "text/html"
      X-Threat-Level: "DANGEROUS"
    body: "<html><body><h1>Blocked</h1></body></html>"
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

A scenario with no `when:` block matches every request — useful as a catch-all default.
All conditions in a `when:` block must match simultaneously (logical AND).

### Delay

- **Static**: `delay: 500ms` — always waits exactly 500 ms.
- **Range**: `delay: 300ms-1500ms` — waits a random duration in [300 ms, 1500 ms].

Any Go duration unit is accepted: `ms`, `s`, `m`, `h`.

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
      body: "File flagged but allowed."
      delay: 200ms-600ms

    - weight: 5
      status: 200
      http_status: 403
      set:
        X-Threat-Level: "DANGEROUS"
        Content-Type: "text/html"
      body: "<html><body><h1>Blocked</h1></body></html>"
      delay: 300ms-1s
```

### `body` vs `body_file`

- `body: |` — inline body as a YAML literal block string.
- `body_file: "./path/to/file"` — load body from a file on disk (relative to process working
  directory). Useful for large or binary responses. Takes precedence if both are set.

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
  set:
    Content-Type: "text/plain"
    X-Threat-Level: "SUSPICIOUS"
  body: "Executable file blocked by policy."
  delay: 100ms-500ms
  priority: 10
```

**3. Multiple conditions (AND logic):**
```yaml
block-email-attachment:
  method: RESPMOD
  when:
    Content-Type: "re:(?i)message/rfc822"
    X-Service-Name: "re:(?i)mail-gateway"
  status: 200
  http_status: 403
  set:
    X-Block-Reason: "Malicious email attachment"
  body: "Email attachment blocked."
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
