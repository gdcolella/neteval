package speedtest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"time"
)

// IperfServer manages an iperf3 server instance for an agent.
type IperfServer struct {
	Port int
	cmd  *exec.Cmd
}

// StartIperfServer starts an iperf3 server on an available port.
func StartIperfServer(ctx context.Context) (*IperfServer, error) {
	// Find a free port
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cmd := exec.CommandContext(ctx, "iperf3", "-s", "-p", fmt.Sprintf("%d", port), "--one-off")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start iperf3 server: %w (is iperf3 installed?)", err)
	}

	// Give iperf3 a moment to bind
	time.Sleep(200 * time.Millisecond)

	log.Printf("iperf3 server listening on port %d", port)
	return &IperfServer{Port: port, cmd: cmd}, nil
}

// Stop kills the iperf3 server.
func (s *IperfServer) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}

// Wait waits for the server process to exit (after --one-off handles one client).
func (s *IperfServer) Wait() {
	if s.cmd != nil {
		s.cmd.Wait()
	}
}

// IperfResult is the parsed output of an iperf3 test.
type IperfResult struct {
	BitsPerSec float64
	Retransmits int
	DurationMs int64
	Error      string
}

// iperfJSON is the subset of iperf3's JSON output we care about.
type iperfJSON struct {
	End struct {
		SumSent struct {
			BitsPerSecond float64 `json:"bits_per_second"`
			Retransmits   int     `json:"retransmits"`
			Seconds       float64 `json:"seconds"`
		} `json:"sum_sent"`
		SumReceived struct {
			BitsPerSecond float64 `json:"bits_per_second"`
			Seconds       float64 `json:"seconds"`
		} `json:"sum_received"`
	} `json:"end"`
	Error string `json:"error"`
}

// RunIperfClient runs iperf3 as a client against a target and returns the results.
// direction: "upload" means client sends (default iperf3 behavior),
// "download" means client receives (-R flag).
func RunIperfClient(targetIP string, targetPort int, durationSec int, direction string, parallel int) (*IperfResult, error) {
	if durationSec <= 0 {
		durationSec = 10
	}
	if parallel <= 0 {
		parallel = 4 // iperf3 default is 1, but 4 parallel streams saturates the link better
	}

	args := []string{
		"-c", targetIP,
		"-p", fmt.Sprintf("%d", targetPort),
		"-t", fmt.Sprintf("%d", durationSec),
		"-P", fmt.Sprintf("%d", parallel),
		"-J", // JSON output
	}

	if direction == "download" {
		args = append(args, "-R") // reverse mode: server sends, client receives
	}

	cmd := exec.Command("iperf3", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Try to parse JSON error from iperf3
		var j iperfJSON
		if jsonErr := json.Unmarshal(out, &j); jsonErr == nil && j.Error != "" {
			return &IperfResult{Error: j.Error}, nil
		}
		return nil, fmt.Errorf("iperf3 client failed: %w: %s", err, string(out))
	}

	var j iperfJSON
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("parse iperf3 output: %w", err)
	}

	if j.Error != "" {
		return &IperfResult{Error: j.Error}, nil
	}

	// For upload, use sum_sent; for download (reverse), use sum_received
	var bps float64
	var dur float64
	var retransmits int
	if direction == "download" {
		bps = j.End.SumReceived.BitsPerSecond
		dur = j.End.SumReceived.Seconds
	} else {
		bps = j.End.SumSent.BitsPerSecond
		dur = j.End.SumSent.Seconds
		retransmits = j.End.SumSent.Retransmits
	}

	return &IperfResult{
		BitsPerSec:  bps,
		Retransmits: retransmits,
		DurationMs:  int64(dur * 1000),
	}, nil
}

// HasIperf3 checks if iperf3 is available on this system.
func HasIperf3() bool {
	_, err := exec.LookPath("iperf3")
	return err == nil
}
