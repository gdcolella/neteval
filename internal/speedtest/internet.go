package speedtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InternetResult holds results from an internet speed test.
type InternetResult struct {
	DownloadBps float64 `json:"download_bps"`
	UploadBps   float64 `json:"upload_bps"`
	LatencyMs   float64 `json:"latency_ms"`
	Error       string  `json:"error,omitempty"`
}

// RunInternetTest measures download and upload speed against Cloudflare.
func RunInternetTest(ctx context.Context) (*InternetResult, error) {
	result := &InternetResult{}

	// Measure latency
	latency, err := measureLatency(ctx)
	if err == nil {
		result.LatencyMs = float64(latency.Milliseconds())
	}

	// Download test: fetch a 25MB payload from Cloudflare
	dlBps, err := measureDownload(ctx)
	if err != nil {
		return nil, fmt.Errorf("download test: %w", err)
	}
	result.DownloadBps = dlBps

	// Upload test: POST random data to Cloudflare
	ulBps, err := measureUpload(ctx)
	if err != nil {
		return nil, fmt.Errorf("upload test: %w", err)
	}
	result.UploadBps = ulBps

	return result, nil
}

func measureLatency(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://speed.cloudflare.com/__down?bytes=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return time.Since(start), nil
}

func measureDownload(ctx context.Context) (float64, error) {
	// Download 25MB
	const size = 25 * 1024 * 1024
	url := fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", size)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	n, _ := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)

	if n == 0 {
		return 0, fmt.Errorf("no data received")
	}

	return float64(n*8) / elapsed.Seconds(), nil
}

func measureUpload(ctx context.Context) (float64, error) {
	// Upload 10MB
	const size = 10 * 1024 * 1024

	data := make([]byte, size)
	rand.Read(data)

	pr, pw := io.Pipe()
	go func() {
		pw.Write(data)
		pw.Close()
	}()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://speed.cloudflare.com/__up", pr)
	req.ContentLength = size

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	elapsed := time.Since(start)

	return float64(size*8) / elapsed.Seconds(), nil
}
