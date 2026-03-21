package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	AuthToken      string
	useIperf       bool
	speedListener  net.Listener
	iperfMu        sync.Mutex // serialize iperf3 server starts
	conn           *websocket.Conn
}

// New creates a new agent.
func New(coordinatorURL string) (*Agent, error) {
	hostname, _ := os.Hostname()

	a := &Agent{
		CoordinatorURL: coordinatorURL,
		Hostname:       hostname,
		useIperf:       true,
	}

	// Ensure iperf3 is available (install if missing)
	if err := speedtest.EnsureIperf3(coordinatorURL); err != nil {
		log.Printf("WARNING: %v — falling back to built-in TCP test", err)
		a.useIperf = false
	}

	if a.useIperf {
		log.Println("using iperf3 for speed tests")
		srv, err := speedtest.StartIperfServer(context.Background())
		if err != nil {
			return nil, fmt.Errorf("start iperf3 server: %w", err)
		}
		a.SpeedPort = srv.Port
		go srv.Wait()
	} else {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, fmt.Errorf("start speed server: %w", err)
		}
		a.speedListener = ln
		a.SpeedPort = ln.Addr().(*net.TCPAddr).Port
		go a.acceptSpeedConnections()
	}

	return a, nil
}

// restartIperfServer starts a fresh iperf3 server (needed after --one-off exits).
func (a *Agent) restartIperfServer() {
	a.iperfMu.Lock()
	defer a.iperfMu.Unlock()

	srv, err := speedtest.StartIperfServer(context.Background())
	if err != nil {
		log.Printf("iperf3 server restart failed: %v", err)
		return
	}
	a.SpeedPort = srv.Port
	go srv.Wait()
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
	if a.AuthToken != "" {
		wsURL += "?token=" + a.AuthToken
	}
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial coordinator: %w", err)
	}
	defer conn.CloseNow()
	a.conn = conn

	conn.SetReadLimit(1 << 20)

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
	case protocol.MsgPrepareServer:
		a.handlePrepareServer(ctx)
	case protocol.MsgUpdateAgent:
		a.handleSelfUpdate()
	case protocol.MsgHeartbeat:
		wsjson.Write(ctx, a.conn, protocol.Envelope{Type: protocol.MsgHeartbeat})
	}
}

func (a *Agent) handlePrepareServer(ctx context.Context) {
	if a.useIperf {
		a.restartIperfServer()
	}
	// Report ready with current speed port
	wsjson.Write(ctx, a.conn, protocol.Envelope{
		Type:    protocol.MsgServerReady,
		Payload: map[string]interface{}{"speed_port": a.SpeedPort},
	})
}

func (a *Agent) handleMeshTest(ctx context.Context, payload interface{}) {
	data, _ := json.Marshal(payload)
	var cmd protocol.RunMeshTestPayload
	json.Unmarshal(data, &cmd)

	duration := time.Duration(cmd.DurationMs) * time.Millisecond
	if duration <= 0 {
		duration = config.DefaultTestDuration
	}

	tr := protocol.TestResult{
		SourceID:   a.ID,
		SourceName: a.Hostname,
		TargetID:   cmd.TargetID,
		TestType:   "mesh",
		Direction:  cmd.Direction,
		Timestamp:  time.Now(),
	}

	if a.useIperf {
		result, err := speedtest.RunIperfClient(
			cmd.TargetIP, cmd.TargetPort,
			int(duration.Seconds()), cmd.Direction, 4,
		)
		if err != nil {
			tr.Error = err.Error()
		} else if result.Error != "" {
			tr.Error = result.Error
		} else {
			tr.BitsPerSec = result.BitsPerSec
			tr.DurationMs = result.DurationMs
		}

		// The target's iperf3 --one-off server exited; it needs to restart.
		// We notify via a separate mechanism (the target agent restarts its own server
		// when it receives the next test command). For now, we just move on.
	} else {
		addr := fmt.Sprintf("%s:%d", cmd.TargetIP, cmd.TargetPort)
		result, err := speedtest.RunClient(addr, cmd.Direction, duration)
		if err != nil {
			tr.Error = err.Error()
		} else {
			tr.BitsPerSec = result.BitsPerSec
			tr.DurationMs = result.DurationMs
		}
	}

	wsjson.Write(ctx, a.conn, protocol.Envelope{
		Type:    protocol.MsgTestResult,
		Payload: tr,
	})
}

func (a *Agent) handleInternetTest(ctx context.Context) {
	result, err := speedtest.RunInternetTest(ctx)

	dlResult := protocol.TestResult{
		SourceID:   a.ID,
		SourceName: a.Hostname,
		TestType:   "internet",
		Direction:  "download",
		Timestamp:  time.Now(),
	}
	if err != nil {
		dlResult.Error = err.Error()
	} else {
		dlResult.BitsPerSec = result.DownloadBps
	}
	wsjson.Write(ctx, a.conn, protocol.Envelope{
		Type:    protocol.MsgTestResult,
		Payload: dlResult,
	})

	ulResult := protocol.TestResult{
		SourceID:   a.ID,
		SourceName: a.Hostname,
		TestType:   "internet",
		Direction:  "upload",
		Timestamp:  time.Now(),
	}
	if err != nil {
		ulResult.Error = err.Error()
	} else {
		ulResult.BitsPerSec = result.UploadBps
	}
	wsjson.Write(ctx, a.conn, protocol.Envelope{
		Type:    protocol.MsgTestResult,
		Payload: ulResult,
	})
}

func (a *Agent) handleSelfUpdate() {
	log.Println("self-update requested, downloading new binary...")

	coordBase := a.CoordinatorURL
	httpURL := strings.Replace(coordBase, "ws://", "http://", 1)
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	binaryURL := httpURL + "/api/binary"

	resp, err := http.Get(binaryURL)
	if err != nil {
		log.Printf("self-update: download failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("self-update: server returned %d", resp.StatusCode)
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Printf("self-update: can't find self: %v", err)
		return
	}

	dir := filepath.Dir(exePath)
	base := filepath.Base(exePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	timestamp := time.Now().Format("20060102-150405")
	newName := fmt.Sprintf("%s-%s%s", name, timestamp, ext)
	newPath := filepath.Join(dir, newName)

	f, err := os.Create(newPath)
	if err != nil {
		log.Printf("self-update: can't create file %s: %v", newPath, err)
		return
	}

	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(newPath)
		log.Printf("self-update: download error: %v", err)
		return
	}

	os.Chmod(newPath, 0755)

	// Try in-place replace (works on Linux/macOS, may fail on Windows)
	if err := os.Rename(newPath, exePath); err == nil {
		log.Println("self-update: replaced in-place, restarting...")
		cmd := exec.Command(exePath, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Start()
		os.Exit(0)
	}

	// Fallback: keep the new file and tell the user
	log.Printf("self-update: new version downloaded to: %s", newPath)
	log.Printf("self-update: stop this agent, replace %s with the new file, and restart", base)
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
