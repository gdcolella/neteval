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
	coordMode := flag.Bool("coordinator", false, "Run as coordinator (default if no flags)")
	agentMode := flag.Bool("agent", false, "Run as agent")
	followMode := flag.Bool("follow", false, "Auto-discover coordinator on the LAN and connect as agent")
	coordAddr := flag.String("coordinator-addr", "", "Coordinator address for agent mode (e.g. 192.168.1.10:8080)")
	port := flag.Int("port", config.DefaultPort, "Listen port for coordinator")
	tlsCert := flag.String("tls-cert", "", "Path to TLS certificate file")
	tlsKey := flag.String("tls-key", "", "Path to TLS key file")
	authToken := flag.String("auth-token", "", "Shared auth token for API/agent access")
	noBrowser := flag.Bool("no-browser", false, "Don't auto-open browser on startup")
	flag.Parse()

	// Default to coordinator mode if no flags specified
	if !*coordMode && !*agentMode && !*followMode {
		*coordMode = true
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *coordMode {
		runCoordinator(ctx, *port, *tlsCert, *tlsKey, *authToken, *noBrowser)
	} else if *followMode {
		runFollower(ctx, *authToken)
	} else if *agentMode {
		if *coordAddr == "" {
			fmt.Fprintln(os.Stderr, "error: --coordinator-addr is required in agent mode")
			os.Exit(1)
		}
		runAgent(ctx, *coordAddr, *authToken)
	}
}

func runCoordinator(ctx context.Context, port int, tlsCert, tlsKey, authToken string, noBrowser bool) {
	c := coordinator.New(port)
	c.TLSCert = tlsCert
	c.TLSKey = tlsKey
	c.AuthToken = authToken

	// Open SQLite store for result persistence
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

	// Broadcast presence so followers can find us
	go discover.BroadcastPresence(ctx, port)

	// Start a local agent that connects back to this coordinator
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

	// Auto-open browser
	if !noBrowser {
		go func() {
			time.Sleep(800 * time.Millisecond)
			scheme := "http"
			if tlsCert != "" {
				scheme = "https"
			}
			url := fmt.Sprintf("%s://localhost:%d", scheme, port)
			openBrowser(url)
		}()
	}

	if err := c.Run(ctx); err != nil {
		log.Fatalf("coordinator: %v", err)
	}
}

func runFollower(ctx context.Context, authToken string) {
	log.Println("searching for NetEval coordinator on the network...")

	coordAddr, err := discover.ListenForCoordinator(ctx)
	if err != nil {
		log.Fatalf("could not find coordinator: %v", err)
	}

	runAgent(ctx, coordAddr, authToken)
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
