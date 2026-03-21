package speedtest

import (
	"net"
	"testing"
	"time"
)

func TestTCPSpeedUpload(t *testing.T) {
	// Start a server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// Accept one connection
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		HandleServerConn(conn)
	}()

	// Run upload test for 1 second
	result, err := RunClient(addr, "upload", 1*time.Second)
	if err != nil {
		t.Fatalf("RunClient upload: %v", err)
	}

	if result.Direction != "upload" {
		t.Errorf("Direction = %q, want %q", result.Direction, "upload")
	}
	if result.BitsPerSec <= 0 {
		t.Errorf("BitsPerSec = %f, want > 0", result.BitsPerSec)
	}
	if result.BytesSent <= 0 {
		t.Errorf("BytesSent = %d, want > 0", result.BytesSent)
	}
	if result.DurationMs <= 0 {
		t.Errorf("DurationMs = %d, want > 0", result.DurationMs)
	}

	t.Logf("Upload: %.2f Mbps (%d bytes in %dms)",
		result.BitsPerSec/1e6, result.BytesSent, result.DurationMs)
}

func TestTCPSpeedDownload(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		HandleServerConn(conn)
	}()

	result, err := RunClient(addr, "download", 1*time.Second)
	if err != nil {
		t.Fatalf("RunClient download: %v", err)
	}

	if result.Direction != "download" {
		t.Errorf("Direction = %q, want %q", result.Direction, "download")
	}
	if result.BitsPerSec <= 0 {
		t.Errorf("BitsPerSec = %f, want > 0", result.BitsPerSec)
	}

	t.Logf("Download: %.2f Mbps (%d bytes in %dms)",
		result.BitsPerSec/1e6, result.BytesSent, result.DurationMs)
}

func TestTCPSpeedConnectRefused(t *testing.T) {
	_, err := RunClient("127.0.0.1:1", "upload", 1*time.Second)
	if err == nil {
		t.Error("expected error connecting to closed port")
	}
}

func TestTCPSpeedMultipleClients(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// Accept multiple connections
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go HandleServerConn(conn)
		}
	}()

	// Run 3 concurrent tests
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			_, err := RunClient(addr, "upload", 500*time.Millisecond)
			errs <- err
		}()
	}

	for i := 0; i < 3; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent client %d: %v", i, err)
		}
	}
}

func TestTestHeaderDirection(t *testing.T) {
	tests := []struct {
		dir string
	}{
		{"upload"},
		{"download"},
	}

	for _, tc := range tests {
		header := TestHeader{
			Direction:  tc.dir,
			DurationMs: 1000,
			BufSize:    65536,
		}

		if header.Direction != tc.dir {
			t.Errorf("Direction = %q, want %q", header.Direction, tc.dir)
		}
	}
}
