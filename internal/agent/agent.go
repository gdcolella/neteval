package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"time"

	"neteval/internal/config"
	"neteval/internal/protocol"
	"neteval/internal/speedtest"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Agent connects to a coordinator and runs speed tests on command.
type Agent struct {
	CoordinatorURL string
	ID             string
	Hostname       string
	SpeedPort      int
	speedListener  net.Listener
	conn           *websocket.Conn
}

// New creates a new agent.
func New(coordinatorURL string) (*Agent, error) {
	hostname, _ := os.Hostname()

	a := &Agent{
		CoordinatorURL: coordinatorURL,
		Hostname:       hostname,
	}

	// Start TCP speed test server on a random port
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, fmt.Errorf("start speed server: %w", err)
	}
	a.speedListener = ln
	a.SpeedPort = ln.Addr().(*net.TCPAddr).Port

	go a.acceptSpeedConnections()

	return a, nil
}

func (a *Agent) acceptSpeedConnections() {
	for {
		conn, err := a.speedListener.Accept()
		if err != nil {
			return
		}
		go speedtest.HandleServerConn(conn)
	}
}

// Run connects to the coordinator and processes commands.
func (a *Agent) Run(ctx context.Context) error {
	delay := config.ReconnectBaseDelay

	for {
		err := a.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("disconnected from coordinator: %v, reconnecting in %v", err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay = delay * 2
		if delay > config.ReconnectMaxDelay {
			delay = config.ReconnectMaxDelay
		}
	}
}

func (a *Agent) connectAndServe(ctx context.Context) error {
	wsURL := a.CoordinatorURL + "/ws/agent"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial coordinator: %w", err)
	}
	defer conn.CloseNow()
	a.conn = conn

	// Increase read limit for large messages
	conn.SetReadLimit(1 << 20) // 1MB

	// Register with coordinator
	localIP := getOutboundIP()
	info := protocol.AgentInfo{
		Hostname:  a.Hostname,
		IP:        localIP,
		SpeedPort: a.SpeedPort,
		OS:        runtime.GOOS,
	}

	err = wsjson.Write(ctx, conn, protocol.Envelope{
		Type:    protocol.MsgAgentRegister,
		Payload: info,
	})
	if err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	// Message loop
	for {
		var env protocol.Envelope
		err := wsjson.Read(ctx, conn, &env)
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		go a.handleMessage(ctx, env)
	}
}

func (a *Agent) handleMessage(ctx context.Context, env protocol.Envelope) {
	switch env.Type {
	case protocol.MsgRunMeshTest:
		a.handleMeshTest(ctx, env.Payload)
	case protocol.MsgRunInternetTest:
		a.handleInternetTest(ctx)
	case protocol.MsgHeartbeat:
		// respond with heartbeat
		wsjson.Write(ctx, a.conn, protocol.Envelope{Type: protocol.MsgHeartbeat})
	}
}

func (a *Agent) handleMeshTest(ctx context.Context, payload interface{}) {
	data, _ := json.Marshal(payload)
	var cmd protocol.RunMeshTestPayload
	json.Unmarshal(data, &cmd)

	addr := fmt.Sprintf("%s:%d", cmd.TargetIP, cmd.TargetPort)
	duration := time.Duration(cmd.DurationMs) * time.Millisecond
	if duration <= 0 {
		duration = config.DefaultTestDuration
	}

	result, err := speedtest.RunClient(addr, cmd.Direction, duration)

	tr := protocol.TestResult{
		SourceID:   a.ID,
		SourceName: a.Hostname,
		TargetID:   cmd.TargetID,
		TestType:   "mesh",
		Direction:  cmd.Direction,
		Timestamp:  time.Now(),
	}

	if err != nil {
		tr.Error = err.Error()
	} else {
		tr.BitsPerSec = result.BitsPerSec
		tr.DurationMs = result.DurationMs
	}

	wsjson.Write(ctx, a.conn, protocol.Envelope{
		Type:    protocol.MsgTestResult,
		Payload: tr,
	})
}

func (a *Agent) handleInternetTest(ctx context.Context) {
	// Simple internet speed test: download a known file and measure throughput
	// For now, we'll use a basic HTTP download test
	tr := protocol.TestResult{
		SourceID:   a.ID,
		SourceName: a.Hostname,
		TestType:   "internet",
		Direction:  "download",
		Timestamp:  time.Now(),
		Error:      "internet test not yet implemented",
	}

	wsjson.Write(ctx, a.conn, protocol.Envelope{
		Type:    protocol.MsgTestResult,
		Payload: tr,
	})
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
