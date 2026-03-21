# How to Release NetEval

## Quick Release (from this server)

```bash
cd /root/neteval

# 1. Make sure tests pass
CGO_ENABLED=1 go test ./... -timeout 60s

# 2. Commit any changes
git add -A && git commit -m "your message"

# 3. Push to GitHub
git push origin main

# 4. Tag and push — this triggers GitHub Actions CI
git tag v1.0.0          # pick your version
git push origin v1.0.0
```

That's it. GitHub Actions will:
- Cross-compile for Linux (amd64), macOS (amd64 + ARM), Windows (amd64)
- Create a GitHub Release with all 4 binaries attached
- Auto-generate release notes from commits

## Manual Local Build (if CI isn't set up)

```bash
cd /root/neteval

# Linux
CGO_ENABLED=1 go build -ldflags="-s -w" -o neteval-linux-amd64 .

# macOS Intel
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o neteval-darwin-amd64 .

# macOS Apple Silicon  
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o neteval-darwin-arm64 .

# Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o neteval-windows-amd64.exe .
```

**Note:** CGO_ENABLED=0 disables SQLite for cross-compiled builds (no persistence). Use CGO_ENABLED=1 for native builds where SQLite works.

## Running It

```bash
# Start coordinator (opens browser, starts local agent)
./neteval

# Start coordinator on custom port, with auth
./neteval --port 9090 --auth-token mysecret

# Start coordinator with TLS
./neteval --tls-cert cert.pem --tls-key key.pem

# Start as agent connecting to a coordinator
./neteval --agent --coordinator-addr 192.168.1.10:8080

# Don't auto-open browser
./neteval --no-browser
```

## Project Structure

```
/root/neteval/
├── main.go                          # Entry point
├── internal/
│   ├── agent/agent.go               # Agent (connects to coordinator, runs tests)
│   ├── coordinator/
│   │   ├── coordinator.go           # HTTP server + API endpoints
│   │   ├── hub.go                   # WebSocket connection manager
│   │   └── orchestrator.go          # Test scheduling (round-robin)
│   ├── protocol/messages.go         # Shared message types
│   ├── config/config.go             # Constants
│   ├── speedtest/
│   │   ├── tcp.go                   # LAN mesh speed testing
│   │   └── internet.go              # WAN speed testing (Cloudflare)
│   ├── store/store.go               # SQLite persistence
│   └── ad/                          # Active Directory deployment
│       ├── discover.go
│       └── deploy.go
├── web/static/                      # Dashboard UI (embedded)
│   ├── index.html
│   ├── app.js
│   └── style.css
├── .github/workflows/release.yml    # CI/CD
└── tasks.md                         # Feature status
```
