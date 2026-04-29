# AGENTS.md — Operational instructions for `icap-mock`

## Scope
- Applies to: `/Users/user/GolandProjects/ICAPMock`
- Primary sources of truth: Go code under `cmd/icap-mock` and `internal/*`, not prose docs.
- Use this file as execution guidance before edits or releases.

## Repository build/runtime baseline
- Module: `github.com/icap-mock/icap-mock`
- Go toolchain: `go 1.25` in `go.mod`; CI and release jobs expect **Go 1.25 and 1.26** support.
- Main binary path: `cmd/icap-mock`.

## CLI contract (code-truth)
- Root behavior in `cmd/icap-mock/main.go`:
  - `icap-mock --help` / `-h` prints command registry usage and exits `0`.
  - `icap-mock --version` prints version metadata and exits `0`.
  - No subcommand defaults to `server` command.
  - Unknown subcommands print suggestions via Levenshtein and exit `1`.
- Registered subcommands in `main.go`:
  - `server` (default)
  - `replay`
  - `validate-scenarios`
  - `match-test`
  - `assert`
  - `generate`

### `server` command behavior (`cmd/icap-mock/server_cmd.go`)
- Default config loading flow:
  1. Load defaults in config.
  2. Load config file if `-c/--config` provided.
  3. Apply env overrides (`ICAP_*`), then CLI flags in `server` command.
- `--version` in server flow prints and exits.
- `--tui` invokes injected `TUIRunner` if present.
- `--validate` runs validate-only mode (no server startup).
- `--help` for server prints grouped flags under: Global, Server, Logging, Metrics, Mock, Chaos, Storage, Rate Limit, Health, Replay, Plugin, Profiling.
- Important flag groups:
  - Global: `-h/--help`, `-d/--debug`, `-c/--config`, `--version`, `--validate`, `--tui`.
  - Server: `--server.host`, `--server.port`, `-p` alias, `--server.read-timeout`, `--server.write-timeout`, `--server.shutdown-timeout`, `--server.max-connections`, `--server.max-body-size`, `--server.streaming`.
  - Logging: `--logging.level`, `--logging.format`, `--logging.output`, `--logging.max-size`, `--logging.max-backups`, `--logging.max-age` with shorthand aliases in this file.
  - Metrics: `--metrics.enabled`, `--metrics.host`, `--metrics.port`, `--metrics.path`.
  - Mock: `--mock.mode`, `--mock.scenarios-dir`, `--mock.timeout`.
  - Chaos: `--chaos.enabled`, `--chaos.error-rate`, `--chaos.timeout-rate`, `--chaos.min-latency-ms`, `--chaos.max-latency-ms`, `--chaos.connection-drop-rate`.
  - Storage: `--storage.enabled`, `--storage.dir`, `--storage.max-size`, `--storage.rotate`.
  - Rate limit: `--rate-limit.enabled`, `--rate-limit.rps`, `--rate-limit.burst`, `--rate-limit.algorithm`.
  - Health: `--health.enabled`, `--health.port`, `--health.path`, `--health.ready-path`.
  - Replay: `--replay.enabled`, `--replay.speed`.
  - Plugin: `--plugin.enabled`, `--plugin.dir`.
  - Profiling: `--pprof.enabled`.

### Other command snapshots
- `replay`: directory-based request replay, supports `--dir`, `--from`, `--to`, `--method`, `--target`, `--speed`, `--parallel`, `--loop`.
- `validate-scenarios`: scans `./configs/scenarios` (or `--dir`) and validates YAML scenario files.
- `match-test`: resolves a request from `--uri` or `--path`, evaluates matching rules from scenario directory, exits non-zero when no match unless verbose.
- `assert`: fetches Prometheus metrics from `--metrics-url` and checks thresholds (`--min-requests`, `--max-error-rate`, `--scenario-hit`, `--max-p95-ms`).
- `generate`: reads recorded NDJSON requests and outputs generated scenario YAML; `--output` optional (stdout default).

## Configuration precedence and validation
- Config loader precedence in `internal/config/loader.go`: defaults → file (`YAML`/`JSON`) → env.
- Env prefix: `ICAP_`; examples: `ICAP_SERVER_PORT`, `ICAP_LOGGING_LEVEL`.
- `--validate` is implemented in `RunValidateMode` and uses `config.Validator` output.

### Hard-coded validation constraints (`internal/config/validator.go`)
- `logging.level`: `debug|info|warn|error`
- `logging.format`: `json|text`
- `mock.mode`: `echo|mock|script`
- `rate_limit.algorithm`: `token_bucket|sliding_window|sharded_token_bucket`
- `server.port`, `metrics.port`, `health.port`: `1..65535`
- TLS: if enabled, both cert and key must be present and exist on disk.

## Defaults and important runtime values
- `internal/config/config.go` defaults:
  - Server: `0.0.0.0:1344`, read/write `30s`, max-connections `15000`, max-body-size `10485760` (10MB), streaming `true`, idle timeout `60s`, shutdown timeout `30s`.
  - Logging: level `info`, format `json`, output `stdout`, `max_size 100`, `max_backups 5`, `max_age 30`.
  - Metrics: enabled `true`, host `0.0.0.0`, port `9090`, path `/metrics`.
  - Mock: default mode `mock`, timeout `5s`, service ID `icap-mock`.
- Health + metrics endpoints exposed by runtime/server composition:
  - ICAP: `1344`
  - Health: `8080` by default (`/health`, `/ready` from health config)
  - Metrics: `9090` by default (`/metrics`)

## CI / release execution
- CI (`.github/workflows/ci.yml`):
  - branches: `main`, `master`, `develop`.
  - `lint` with `golangci-lint v2.11`.
  - `test` matrix for Go `1.25` and `1.26` using `make test`.
  - `security` depends on lint/test (govulncheck, gosec, trivy) and comments PRs when available.
  - `docker-test`: builds image and checks `/health` endpoint on startup.
- Release (`.github/workflows/release.yml`):
  - triggers on tags matching `[0-9]+.[0-9]+.[0-9]+*`.
  - builds Linux/Windows/darwin artifacts for `amd64/arm64`.
  - changelog generated from previous git tag (or bootstrap "Initial Release").
  - release notes include installation examples for Linux/macOS/Windows and Docker image instructions.
  - image publish job is currently commented out; currently release only uploads native binaries + checksums.

## Docker and local deployment
- Dockerfile: multi-stage build (`golang:1.26-alpine` builder → `alpine:3.19` runtime).
- Dockerfile version metadata args: `VERSION`, `GIT_COMMIT`, `BUILD_DATE`; runtime image uses `ENTRYPOINT ["./icap-mock"]` with `CMD ["--config", "configs/example.yaml"]`.
- Non-root runtime user: uid/gid `1000` (`icapuser`/`icapgroup`).
- `docker-compose.yml` maps `ICAP_PORT:1344`, `HEALTH_PORT:8080`, `METRICS_PORT:9090` and sets env such as `ICAP_*` overrides.

## Helm chart behavior to keep in sync
- Chart entry: `charts/icap-mock`.
- `deployment.yaml` injects `--config` from `.Values.icap.configFile` and includes ConfigMap checksum annotation.
- `configmap.yaml` renders configuration and extra snippets from values.
- `service.yaml` controls service naming (`icap` for single server, `icap-<name>` for multi-server mode) and extra ports.
- `servicemonitor.yaml` is conditional on `.Values.metrics.enabled && .Values.serviceMonitor.enabled`.

## Lint/quality gates
- `.golangci.yml` is strict: go `1.25`, modules readonly download, test files included, many enabled linters.
- `goheader` requires copyright prefix (`Copyright {{YEAR}} ICAP Mock`).

## Known sharp edge
- Version ldflags are case-sensitive:
  - Source vars are `version`, `gitCommit`, `buildDate` in `cmd/icap-mock/cli_flags.go` and printed via `PrintVersion`.
  - Dockerfile currently injects `main.version`/`main.gitCommit`/`main.buildDate`.
  - Release/workflow `build` job currently injects `main.Version` (capital V), so check and align when modifying release/build scripts.

## Editing and docs rules
- Keep examples minimal and runnable.
- Preserve existing command and config semantics when updating docs.
- Prefer explicit values/paths from code over prose inference.
