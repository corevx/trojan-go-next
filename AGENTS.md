# AGENTS.md

Guidance for coding agents working in this repository.

## Project

Trojan-Go-Next is a Go implementation of the Trojan proxy protocol with TLS, WebSocket, smux multiplexing, Shadowsocks AEAD encryption, geo routing, gRPC API, and transparent proxy support.

The module path is `github.com/p4gefau1t/trojan-go`; keep imports on that path unless a repository-wide rename is explicitly requested.

## Commands

Use these commands from the repository root:

```bash
# Full build, CGO disabled, full build tags.
make

# Full test suite. The Shadowsocks env var is required.
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./...

# Single package tests.
SHADOWSOCKS_SF_CAPACITY="-1" go test -v ./tunnel/trojan/

# Race test for focused packages.
SHADOWSOCKS_SF_CAPACITY="-1" go test -race ./tunnel/tls ./tunnel/mux

# Release cross-compilation.
make release

# Lint, matching CI.
golangci-lint run --config=.github/linters/.golangci.yml
```

`make test` already sets `SHADOWSOCKS_SF_CAPACITY="-1"`. Direct `go test ./...` without that variable can fail in Shadowsocks tests.

## CI/CD

Important workflows:

- `.github/workflows/test.yml`: runs `go mod tidy`, `go mod verify`, and `make test` on Ubuntu, Windows, and macOS.
- `.github/workflows/linter.yml`: runs golangci-lint with `.github/linters/.golangci.yml`.
- `.github/workflows/deps-update.yml`: scheduled/manual dependency update, build/test verification, auto PR merge, patch tag, and release build.
- `.github/workflows/dependabot-auto-merge.yml`: auto-approves Dependabot PRs and enables auto-merge for patch/minor updates.
- `.github/dependabot.yml`: weekly Go module and GitHub Actions updates. `github.com/xtaci/smux` major updates and `github.com/txthinking/socks5` updates are intentionally ignored because they need manual migration.

When diagnosing CI, do not assume all failures are caused by the current diff. Check the failing package and the changed files. Shadowsocks timeouts have occurred independently of TLS/mux-only changes.

## Architecture

The core is a layered tunnel stack under `tunnel/`. Each layer registers through package init and implements the interfaces in `tunnel/tunnel.go`:

- `Tunnel`: creates clients and servers.
- `Client`: dials outbound connections.
- `Server`: accepts inbound connections.

Typical client flow:

```text
socks/http -> adapter -> trojan -> shadowsocks -> tls/websocket -> transport
```

Typical server flow:

```text
transport -> tls/websocket -> trojan -> shadowsocks/mux -> freedom/router
```

Proxy mode composition lives in `proxy/`. Build-tag aggregation lives in `component/`.

Configuration is context-based. Config creators are registered with `config.RegisterConfigCreator()` and retrieved by package name through `config.FromContext`.

## Development Rules

- Prefer existing package patterns and tunnel abstractions over new architecture.
- Keep changes scoped to the requested behavior.
- Add tests near the changed package when changing behavior or concurrency.
- For TLS/mux/shadowsocks/networking changes, run the relevant package tests first, then the full suite if feasible.
- Be careful with goroutines and channels in tunnel code; avoid blocking sends in hot paths unless backpressure is intentional and documented.
- Do not update dependencies opportunistically unless the task is dependency maintenance.
- Do not remove or rewrite user changes in a dirty worktree.

## Git And Release

- Commit author convention: `corevx <corevx@users.noreply.github.com>`.
- Use Chinese commit messages that describe intent clearly.
- Commit completed functional stages instead of batching unrelated changes.
- Do not push to remotes unless explicitly asked.
- The GitHub remote used by project notes is `corevx`; example: `git push corevx main`.
- For important user-facing fixes or features, update semantic versioning according to existing project practice and tag only when requested or clearly part of the release task.

## Known Notes

- `go.mod` currently targets Go `1.25.0`; CI uses `actions/setup-go` with Go `1.25`.
- Geo data files are loaded from the binary directory or `TROJAN_GO_LOCATION_ASSET`.
- Integration scenarios live in `test/scenario/`.
- Server fallback behavior is handled by `redirector/`.
- Existing WebSocket race-test behavior may be pre-existing; isolate race reports to the package under investigation before attributing them to a current change.
