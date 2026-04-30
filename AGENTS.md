# AGENTS.md

## Scope
- Keep changes repo-specific. Do not add generic Go advice.
- Main binary: `cmd/icap-mock`.
- Public protocol package: `pkg/icap`.
- Internals live under `internal/`.

## Runtime map
- `cmd/icap-mock/main.go` registers CLI subcommands and defaults to `server` when no subcommand is given.
- `cmd/icap-mock/cli_executor.go` is the main wiring point for config, logging, metrics, rate limiting, storage, scenario registries, processors, routers, and ICAP servers.
- `internal/server` owns listeners and connection handling.
- `internal/router` routes ICAP URI paths, including pattern routes.
- `internal/processor` matches scenarios and builds responses.
- `internal/storage` loads scenarios and stores/replays captured requests.

## Commands agents should prefer
- Build: `make build`
- Local run: `./bin/icap-mock server --config configs/example.yaml`
- Full test suite: `make test`
- Focused test example: `go test -v -race ./cmd/icap-mock -run TestNewServerCommand`
- Format: `make fmt`
- Vet: `make vet`
- Lint: `make lint`
- Full quality pass: `make all`
- Dependency cleanup: `make mod-tidy`
- Coverage UI: `make test-coverage` (requires `make test` first)
- Scenario validation: `go run ./cmd/icap-mock validate-scenarios --dir ./configs/scenarios`
- Scenario matching probe: `go run ./cmd/icap-mock match-test --scenarios ./configs/scenarios/example --path /scan --method REQMOD --verbose`
- Docker build: `docker build -t icap-mock:latest .`
- Docker compose: `docker-compose up -d`
- Docker compose + monitoring: `docker-compose --profile monitoring up -d`

## Config and scenario gotchas
- Config precedence is `defaults < config file < env`; env prefix is `ICAP_`.
- Two config shapes are supported: legacy single-server (`server` + `mock`) and multi-server (`defaults` + `servers`).
- `make run` points at missing `configs/config.yaml`; use `configs/example.yaml` instead.
- Scenario v2 requires `method` and `endpoint` in `defaults` or on each scenario.
- Use `when:` for ICAP envelope headers.
- Use `when_http.headers` for encapsulated HTTP headers like `Content-Type`.
- Scenario priority is numeric: higher priority wins; file order breaks ties.
- If management API auth is unset, startup logs a warning; set `management.token` or `management.token_env`.

## Go / CI constraints
- Module path: `github.com/icap-mock/icap-mock`
- `go.mod` declares `go 1.25.0`.
- CI tests Go `1.25` and `1.26`; Docker and release builds use Go `1.26`.
- Go source files must keep the header `// Copyright 2026 ICAP Mock`.
- `goimports` local prefix is `github.com/icap-mock/icap-mock`.
- `golangci-lint` uses `modules-download-mode: readonly`; if dependencies changed, run `go mod tidy` before `make lint`.
