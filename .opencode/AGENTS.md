# AGENTS.md

## Repo Shape

- Project is using jj-vcs (not git).
- Go module `github.com/rupor-github/gclpr`; `go.mod` requires Go `1.26.3` and records `task`, `staticcheck`, and `npiperelay` as Go tools.
- Main binaries are `./cmd/cli` (`gclpr`) and Windows-only `./cmd/gui` (`gclpr-gui.exe` tray server).
- Core RPC/server code is in `server/`; shared key/frame/file utilities are in `util/`.
- `systray/` is a repo-local fork of `github.com/getlantern/systray` kept for Windows-specific behavior; do not treat it as an untouched upstream vendored dependency.

## Commands

- Prefer `go tool task ...`; Taskfile sets `GOTOOLCHAIN=local+path`, `CGO_ENABLED=0`, and a temp default `GOPATH`.
- `go tool task` or `go tool task debug`: Linux-hosted development build for Windows amd64 GUI and CLI, then compiles all tests and runs lint.
- `go tool task test`: runs tests only in directories containing `*_test.go`, writes coverage under `build/tests_results`, and enables `CGO_ENABLED=1` for CGO-dependent tests.
- Focus tests with `PACKAGES='./server' go tool task test -- -run=TestName`; `PACKAGES` is comma-separated for multiple packages.
- `go tool task lint`: runs `staticcheck` as `GOOS=windows ... ./...`, so lint must pass Windows build-tag paths too.
- `go tool task release`: cross-builds linux/darwin/windows amd64+arm64 archives and generates `gclpr.json`; CI uses exactly this command on tag creation.
- `go tool task tidy` and `go tool task vendor` iterate all release target OS/arch combinations; release builds use `-mod=vendor`, debug builds use `-mod=mod`.

## Generated Files

- `misc/version.go`, `cmd/gui/manifest.xml`, `cmd/gui/resources.rc`, `cmd/gui/resources.syso`, and `gclpr.json` are generated and ignored; avoid hand-editing them.
- `task generate-project-version` uses `GITHUB_REF` tags when present, otherwise `0.0.0-dev`, and embeds the current git hash plus `*` for dirty trees.
- Windows GUI resource generation requires `x86_64-w64-mingw32-windres`; the release CI installs `mingw-w64` and `binutils-mingw-w64-x86-64` for this.

## Verification Gotchas

- Pre-commit hooks are local: `gofmt -l . | grep -v '^vendor/'` on commit/push and `trivy --exit-code 1 fs --ignore-unfixed .` on push.
- `go tool task` installs pre-commit hooks and will fail if `pre-commit` is unavailable; use narrower tasks like `go tool task test` or `go tool task lint` when you only need verification.
- Some tests and compile checks intentionally set `CGO_ENABLED=1`; do not assume the repo is always CGO-free despite Taskfile's default `CGO_ENABLED=0`.

## Runtime/Architecture Notes

- `server.Serve` registers RPC handlers on `rpc.DefaultServer` and is documented as callable only once per process.
- All RPC transport is localhost TCP; non-local use is expected through SSH forwarding, not by binding non-loopback addresses.
- CLI aliases matter: binaries/symlinks named `pbcopy`, `pbpaste`, and `xdg-open` map to `copy`, `paste`, and `open`; `xdg-open` auto-enables OAuth tunneling unless `-tunnel` is set.
- Trusted/client key files live under `$HOME/.gclpr`; tests or manual runs that touch auth behavior may be sensitive to permissions on `key`, `key.pub`, and `trusted`.
