package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"neteval/internal/agent"
	"neteval/internal/config"
	"neteval/internal/coordinator"
	"neteval/internal/discover"
	"neteval/internal/store"
)

func main() {
	port := flag.Int("port", config.DefaultPort, "Listen port for coordinator")
	tlsCert := flag.String("tls-cert", "", "Path to TLS certificate file")
	tlsKey := flag.String("tls-key", "", "Path to TLS key file")
	authToken := flag.String("auth-token", "", "Shared auth token for API/agent access")
	noBrowser := flag.Bool("no-browser", false, "Don't auto-open browser on startup")
	// Hidden flags for manual override (backward compat)
	forceCoord := flag.Bool("coordinator", false, "Force coordinator mode")
	forceAgent := flag.Bool("agent", false, "Force agent mode")
	coordAddr := flag.String("coordinator-addr", "", "Coordinator address (agent mode)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *forceAgent {
		if *coordAddr == "" {
			fmt.Fprintln(os.Stderr, "error: --coordinator-addr is required with --agent")
			os.Exit(1)
		}
		runAgent(ctx, *coordAddr, *authToken)
		return
	}

	if *forceCoord {
		runCoordinator(ctx, *port, *tlsCert, *tlsKey, *authToken, *noBrowser)
		return
	}

	// Auto-detect: look for an existing coordinator on the LAN.
	// If found, join as an agent. Otherwise, become the coordinator.
	log.Println("looking for an existing NetEval coordinator on the network...")

	searchCtx, searchCancel := context.WithTimeout(ctx, 4*time.Second)
	defer searchCancel()

	found, err := discover.ListenForCoordinator(searchCtx)
	if err == nil && found != "" {
		log.Printf("found coordinator at %s — joining as agent", found)
		runAgent(ctx, found, *authToken)
	} else {
		log.Println("no coordinator found — starting as coordinator")
		runCoordinator(ctx, *port, *tlsCert, *tlsKey, *authToken, *noBrowser)
	}
}

func runCoordinator(ctx context.Context, port int, tlsCert, tlsKey, authToken string, noBrowser bool) {
	c := coordinator.New(port)
	c.TLSCert = tlsCert
	c.TLSKey = tlsKey
	c.AuthToken = authToken

	db, err := store.New("neteval.db")
	if err != nil {
		log.Printf("warning: could not open result store: %v (results won't persist)", err)
	} else {
		c.Store = db
		c.Hub.Store = db
		defer db.Close()
		log.Printf("result store opened (neteval.db)")
	}

	c.LoadTargets()
	log.Printf("starting NetEval coordinator on port %d", port)

	// Broadcast presence so other instances auto-join
	go discover.BroadcastPresence(ctx, port)

	// Start a local agent
	go func() {
		time.Sleep(500 * time.Millisecond)
		scheme := "ws"
		if tlsCert != "" {
			scheme = "wss"
		}
		wsURL := fmt.Sprintf("%s://127.0.0.1:%d", scheme, port)
		a, err := agent.New(wsURL)
		if err != nil {
			log.Printf("local agent failed to start: %v", err)
			return
		}
		if authToken != "" {
			a.AuthToken = authToken
		}
		log.Printf("local agent started, participating in mesh tests")
		a.Run(ctx)
	}()

	if !noBrowser {
		go func() {
			time.Sleep(800 * time.Millisecond)
			scheme := "http"
			if tlsCert != "" {
				scheme = "https"
			}
			openBrowser(fmt.Sprintf("%s://localhost:%d", scheme, port))
		}()
	}

	if err := c.Run(ctx); err != nil {
		log.Fatalf("coordinator: %v", err)
	}
}

func runAgent(ctx context.Context, coordAddr, authToken string) {
	wsURL := fmt.Sprintf("ws://%s", coordAddr)
	a, err := agent.New(wsURL)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	a.AuthToken = authToken
	log.Printf("starting NetEval agent, connecting to %s", coordAddr)
	if err := a.Run(ctx); err != nil {
		log.Fatalf("agent: %v", err)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
