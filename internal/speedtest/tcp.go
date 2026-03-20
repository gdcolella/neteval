package speedtest

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"neteval/internal/config"
)

// TestHeader is sent at the start of a speed test TCP connection.
type TestHeader struct {
	Direction  string `json:"direction"`   // "upload" or "download"
	DurationMs int64  `json:"duration_ms"`
	BufSize    int    `json:"buf_size"`
}

// TestAck is the server's response to the header.
type TestAck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Result holds the outcome of a speed test.
type Result struct {
	Direction  string  `json:"direction"`
	BitsPerSec float64 `json:"bits_per_sec"`
	BytesSent  int64   `json:"bytes_sent"`
	DurationMs int64   `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
}

// RunClient connects to a speed test server and runs a throughput test.
func RunClient(addr string, direction string, duration time.Duration) (*Result, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()

	bufSize := config.DefaultBufSize
	header := TestHeader{
		Direction:  direction,
		DurationMs: duration.Milliseconds(),
		BufSize:    bufSize,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(header); err != nil {
		return nil, fmt.Errorf("send header: %w", err)
	}

	dec := json.NewDecoder(conn)
	var ack TestAck
	if err := dec.Decode(&ack); err != nil {
		return nil, fmt.Errorf("read ack: %w", err)
	}
	if !ack.OK {
		return nil, fmt.Errorf("server rejected: %s", ack.Error)
	}

	if direction == "upload" {
		return runSender(conn, bufSize, duration)
	}
	return runReceiver(conn, duration)
}

// HandleServerConn handles an incoming speed test connection on the server side.
func HandleServerConn(conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	var header TestHeader
	if err := dec.Decode(&header); err != nil {
		return
	}

	enc := json.NewEncoder(conn)
	enc.Encode(TestAck{OK: true})

	duration := time.Duration(header.DurationMs) * time.Millisecond
	if duration <= 0 {
		duration = config.DefaultTestDuration
	}
	bufSize := header.BufSize
	if bufSize <= 0 {
		bufSize = config.DefaultBufSize
	}

	if header.Direction == "upload" {
		// Client is uploading, server receives
		runReceiver(conn, duration)
	} else {
		// Client is downloading, server sends
		runSender(conn, bufSize, duration)
	}
}

func runSender(conn net.Conn, bufSize int, duration time.Duration) (*Result, error) {
	buf := make([]byte, bufSize)
	rand.Read(buf) // fill with random data

	deadline := time.Now().Add(duration)
	conn.SetWriteDeadline(deadline)

	var totalBytes int64
	start := time.Now()

	for time.Now().Before(deadline) {
		n, err := conn.Write(buf)
		totalBytes += int64(n)
		if err != nil {
			break
		}
	}

	elapsed := time.Since(start)
	bps := float64(totalBytes*8) / elapsed.Seconds()

	return &Result{
		Direction:  "upload",
		BitsPerSec: bps,
		BytesSent:  totalBytes,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

func runReceiver(conn net.Conn, duration time.Duration) (*Result, error) {
	buf := make([]byte, config.DefaultBufSize)

	// Give a bit of extra time for final reads
	conn.SetReadDeadline(time.Now().Add(duration + 5*time.Second))

	var totalBytes int64
	start := time.Now()

	for {
		n, err := conn.Read(buf)
		totalBytes += int64(n)
		if err != nil {
			if err == io.EOF {
				break
			}
			// Timeout is expected
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			break
		}
	}

	elapsed := time.Since(start)
	bps := float64(totalBytes*8) / elapsed.Seconds()

	return &Result{
		Direction:  "download",
		BitsPerSec: bps,
		BytesSent:  totalBytes,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}
