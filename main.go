package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"neteval/internal/agent"
	"neteval/internal/config"
	"neteval/internal/coordinator"
)

func main() {
	coordMode := flag.Bool("coordinator", false, "Run as coordinator (default if no flags)")
	agentMode := flag.Bool("agent", false, "Run as agent")
	coordAddr := flag.String("coordinator-addr", "", "Coordinator address for agent mode (e.g. 192.168.1.10:8080)")
	port := flag.Int("port", config.DefaultPort, "Listen port for coordinator")
	flag.Parse()

	// Default to coordinator mode if no flags specified
	if !*coordMode && !*agentMode {
		*coordMode = true
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if *coordMode {
		runCoordinator(ctx, *port)
	} else if *agentMode {
		if *coordAddr == "" {
			fmt.Fprintln(os.Stderr, "error: --coordinator-addr is required in agent mode")
			os.Exit(1)
		}
		runAgent(ctx, *coordAddr)
	}
}

func runCoordinator(ctx context.Context, port int) {
	c := coordinator.New(port)
	log.Printf("starting NetEval coordinator on port %d", port)
	if err := c.Run(ctx); err != nil {
		log.Fatalf("coordinator: %v", err)
	}
}

func runAgent(ctx context.Context, coordAddr string) {
	wsURL := fmt.Sprintf("ws://%s", coordAddr)
	a, err := agent.New(wsURL)
	if err != nil {
		log.Fatalf("agent: %v", err)
	}
	log.Printf("starting NetEval agent, connecting to %s", coordAddr)
	if err := a.Run(ctx); err != nil {
		log.Fatalf("agent: %v", err)
	}
}
