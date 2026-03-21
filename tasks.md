# NetEval - Feature Status

## Core Functionality

- [x] **Coordinator self-agent** — `main.go` starts a local agent goroutine that connects back to the coordinator
- [x] **Internet speed testing** — `internal/speedtest/internet.go` tests against Cloudflare (25MB down, 10MB up, latency)
- [x] **Test result persistence** — SQLite via `internal/store/store.go`. Auto-saves every result. Run summaries, history retrieval, cross-restart persistence.

## Active Directory Deployment

- [x] **AD machine discovery** — `internal/ad/discover.go` — LDAP via PowerShell on Windows, ldapsearch/subnet scan on Linux
- [x] **Credential input via web UI** — Dashboard has AD deployment modal
- [x] **Remote binary copy** — `internal/ad/deploy.go` — SMB (ADMIN$ share) on Windows, smbclient from Linux
- [x] **Remote agent launch** — PsExec → WMI fallback on Windows, winexe → impacket on Linux
- [x] **Deployment status tracking** — `deploy_status` WebSocket messages broadcast to dashboard

## Dashboard Improvements

- [x] **Internet speed bar chart** — Table with proportional speed bars per agent (download + upload)
- [x] **Result history view** — `/api/history/runs` + `/api/history/run?id=` + History tab in dashboard
- [x] **Export results** — `/api/results/export?format=csv|json`
- [x] **Auto-refresh agent list** — Polls `/api/agents` every 10s, re-renders on change
- [x] **Test progress bar** — Progress bar with percentage and status text

## Security & Reliability

- [x] **TLS support** — `--tls-cert` and `--tls-key` flags, `ListenAndServeTLS` in coordinator
- [x] **Dashboard authentication** — Auth middleware checks query param, header, or cookie token
- [x] **Agent authentication** — Token passed as query param on WebSocket connect
- [x] **Graceful shutdown** — `signal.NotifyContext` + `srv.Shutdown` with configurable timeout

## Platform & Distribution

- [ ] **Windows service mode** — Can't test on Linux (deferred)
- [x] **macOS ARM build** — `darwin/arm64` in CI matrix and local cross-compile
- [x] **GitHub Actions CI** — `.github/workflows/release.yml` — builds 4 platform binaries on tag push
- [x] **Auto-open browser** — `openBrowser()` in `main.go` — respects `--no-browser` flag

## Testing — 69 tests across 7 packages

- [x] `internal/config` — constant validation, Options struct
- [x] `internal/protocol` — message type constants, JSON serialization roundtrips for all payloads
- [x] `internal/speedtest` — TCP upload/download throughput, connection refused, concurrent clients
- [x] `internal/coordinator/hub` — hub lifecycle, add/get/clear results, defensive copy
- [x] `internal/coordinator/orchestrator` — round-robin pairing (2-6 agents), bye filtering, pair uniqueness
- [x] `internal/coordinator/integration` — HTTP endpoints, auth middleware (5 auth methods), graceful start/stop, mesh test validation
- [x] `internal/ad` — domainToDN, LDAP output parsing, struct validation
- [x] `internal/store` — CRUD, batch insert, run summaries, concurrent access, persistence across reopen, empty db, error results filtered from summaries
