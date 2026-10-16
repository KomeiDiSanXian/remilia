# AGENTS.md

Remilia: Go 1.27 multi-platform bot framework. Root module `github.com/KomeiDiSanXian/remilia` is the library; `cmd/bot` is the reference bot (own `go.mod`, `replace` to `../..`).

## Module boundaries

- `go.work` covers `.`, `cmd/bot`, `examples/httpclient-demo`. From the root, `go build/test ./...` matches only the root module — build/test `cmd/bot` from its own directory.
- `examples/showcase/wasm` is intentionally outside the workspace (TinyGo/WASI); do not add it to `go.work`.
- Layout: `core/` engine (COW) + context + fsm + permission, `platform/` adapters, `plugin/` DI/plugin runtime, `builtin/` framework-shipped plugins, `cmd/bot/plugins/` app-specific plugins, `infra/` toolkit, `api/` admin REST server, `dashboard/` React SPA, `desktop/` Tauri shell, `tests/` integration/benchmark/chaos/fuzzing. Deep design notes: `docs/notes/`.
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
- `CHANGELOG.md` gets a detailed Chinese entry per release; update it with user-visible changes. The `vX.Y.Z` section is also the GitHub Release body: the release workflow extracts it with `scripts/release-notes.mjs` and only falls back to the generated commit list when the section is missing — write the section before tagging.
- Release assets are attested with `actions/attest@v4`: `id-token: write` + `attestations: write` must be granted in **both** `release.yml` and the reusable `android.yml` (a missing grant only surfaces once a tag is pushed). Users verify with `gh attestation verify <file> --repo KomeiDiSanXian/remilia`.
- Container images go through `dockers_v2` (buildx, `platforms: linux/amd64, linux/arm64, linux/arm/v7`); `disable: "{{ .IsSnapshot }}"` keeps nightly image-free (snapshot image builds would need QEMU). The Dockerfile must `COPY $TARGETPLATFORM/remilia` — the v2 build context is per-platform, unlike the flat legacy one.
- Metric tests must assert deltas around each call, never absolute values of package-global Prometheus counters (they fail under `go test -count=N`).
- Gitignored, don't commit: `config.yaml`, `cmd/bot/dashboarddist/`, `data/`, `logs/`, `profiles/`, `site/`, `MEMORY.md` (local-only agent notebook worth consulting for recent work notes).
- Line endings are pinned to LF by `.gitattributes` (`*.bat`/`*.cmd` keep CRLF); don't re-normalize.

## Desktop & WASM

- Tauri sidecar must be named `remilia-bot-<rust-target-triple>[.exe]` in `desktop/src-tauri/binaries/`; use `make desktop-sidecar` / `desktop-prepare`.
- Tauri deps are paired by minor: `desktop/package.json` and `desktop/src-tauri/Cargo.toml` both pin `~x.y` for `@tauri-apps/api`↔`tauri`, `plugin-shell`, `plugin-updater`, `plugin-process`, because `tauri build` aborts on a major/minor mismatch (`version mismatched Tauri packages`). Bump both sides in the same commit — a one-sided lock refresh must never cross a minor (rationale and table: `desktop/README.md`).
- PRs touching `desktop/**` (or `.github/workflows/android.yml`) run the arm64 APK build as a check; `v*` tags publish the APK. Trigger matrix, signing and manual republish: `desktop/README.md`.
- `make build-wasm-test` needs TinyGo + Go 1.23; sample `.wasm` fixtures are committed.

## Known flake

- Windows CI has crashed twice with `Exception 0xc0000005` (package `plugin` at test teardown; `builtin/ai`'s `TestNewGormSessionStorePanicsWithNil`, whose raw nil dereference has since become an explicit error). Treat a recurrence as runtime/platform-level first and simply rerun the failed job — the same commit passes on retry; the job sets `GOTRACEBACK: system` and uploads `windows-test-logs`.
- For the real test output read that artifact, not the log: `gh run download <run-id> -n windows-test-logs` → `windows-test.log` (the `gh run view --job --log` stream is tab-split and shreds `go test` output).
