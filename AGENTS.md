# AGENTS.md

Remilia: Go 1.27 multi-platform bot framework. Root module `github.com/KomeiDiSanXian/remilia` is the library; `cmd/bot` is the reference bot (own `go.mod`, `replace` to `../..`).

## Module boundaries

- `go.work` covers `.`, `cmd/bot`, `examples/httpclient-demo`. From the root, `go build/test ./...` matches only the root module — build/test `cmd/bot` from its own directory.
- `examples/showcase/wasm` is intentionally outside the workspace (TinyGo/WASI); do not add it to `go.work`.
- Layout: `core/` engine (COW) + context + fsm + permission, `platform/` adapters, `plugin/` DI/plugin runtime, `builtin/` framework-shipped plugins, `cmd/bot/plugins/` app-specific plugins, `infra/` toolkit, `api/` admin REST server, `dashboard/` Vue SPA, `desktop/` Tauri shell, `tests/` integration/benchmark/chaos/fuzzing. Deep design notes: `docs/notes/`.
- Dependency rule (`builtin/README.md`): `core/` and `infra/` must never import `builtin/`; layer-3 business plugins must not be imported by layers 1–2.

## Build & test

- Root build: `go build ./...`. Full tests: `make test` = `go test -count=1 -race -shuffle=on -p 4 -timeout 180s ./...` (`-p 4` is deliberate: it prevents port/CPU-contention flakiness). Single test: `go test ./core/engine -run TestName -v`. Add `-short` to skip slow chaos tests.
- `cmd/bot` embeds the SPA via `//go:embed all:dashboarddist`, so builds/tests there fail on a clean clone. Stub it as CI does: `mkdir -p cmd/bot/dashboarddist && touch cmd/bot/dashboarddist/.stub`, or build the real SPA with `make dashboard-build`.
- cmd/bot tests: `cd cmd/bot && go test -count=1 -race -timeout 300s ./...`
- Live network tests use `//go:build network` (`platform/qq`, `cmd/bot/plugins/pic`, `.../sauce`) and need real credentials; skipped by default.
- Verify: `go vet ./...` and `golangci-lint run ./...`. The `staticcheck` CI job is disabled and `SA4023`/`QF1012` are excluded because staticcheck crashes on Go 1.27 generics — don't remove those suppressions.
- CI's fmt job runs `go fmt ./...` + `go fix ./...` and auto-opens an `auto/fmt-fix` PR on master; run both locally before pushing.
- Build flags: `-X github.com/KomeiDiSanXian/remilia.Version=...` and `-X main.commit`/`main.date`; those two vars must stay in the `cmd/bot/main.go` package. `version.go` `const Version` is the single version source; releases are `v*` tags via GoReleaser.

## Conventions

- Commit messages, PR titles, and release notes are English. Code comments and user docs are Chinese — match the surrounding language.
- `CHANGELOG.md` gets a detailed Chinese entry per release; update it with user-visible changes.
- Metric tests must assert deltas around each call, never absolute values of package-global Prometheus counters (they fail under `go test -count=N`).
- Gitignored, don't commit: `config.yaml`, `cmd/bot/dashboarddist/`, `data/`, `logs/`, `profiles/`, `site/`, `MEMORY.md` (local-only agent notebook worth consulting for recent work notes).
- Line endings are pinned to LF by `.gitattributes` (`*.bat`/`*.cmd` keep CRLF); don't re-normalize.

## Desktop & WASM

- Tauri sidecar must be named `remilia-bot-<rust-target-triple>[.exe]` in `desktop/src-tauri/binaries/`; use `make desktop-sidecar` / `desktop-prepare`.
- `make build-wasm-test` needs TinyGo + Go 1.23; sample `.wasm` fixtures are committed.

## Known flake

- Windows CI once crashed with `Exception 0xc0000005` in package `plugin` at test teardown (not reproducible locally). Treat a recurrence as runtime/platform-level first; the job sets `GOTRACEBACK: system` and uploads `windows-test-logs`.
