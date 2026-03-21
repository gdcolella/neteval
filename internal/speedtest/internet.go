package speedtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"sync"
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

	// Measure latency (average of 3 pings)
	latency, err := measureLatency(ctx)
	if err == nil {
		result.LatencyMs = float64(latency.Milliseconds())
	}

	// Download test: 4 parallel streams, 25MB each = 100MB total
	dlBps, err := measureDownloadParallel(ctx, 4, 25*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("download test: %w", err)
	}
	result.DownloadBps = dlBps

	// Upload test: 4 parallel streams, 10MB each = 40MB total
	ulBps, err := measureUploadParallel(ctx, 4, 10*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("upload test: %w", err)
	}
	result.UploadBps = ulBps

	return result, nil
}

func measureLatency(ctx context.Context) (time.Duration, error) {
	var total time.Duration
	var count int
	for i := 0; i < 3; i++ {
		start := time.Now()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://speed.cloudflare.com/__down?bytes=0", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		total += time.Since(start)
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("all pings failed")
	}
	return total / time.Duration(count), nil
}

func measureDownloadParallel(ctx context.Context, streams int, bytesPerStream int) (float64, error) {
	url := fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", bytesPerStream)

	var mu sync.Mutex
	var totalBytes int64
	var wg sync.WaitGroup
	var firstErr error

	start := time.Now()

	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			defer resp.Body.Close()
			n, _ := io.Copy(io.Discard, resp.Body)
			mu.Lock()
			totalBytes += n
			mu.Unlock()
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	if totalBytes == 0 {
		if firstErr != nil {
			return 0, firstErr
		}
		return 0, fmt.Errorf("no data received")
	}

	return float64(totalBytes*8) / elapsed.Seconds(), nil
}

func measureUploadParallel(ctx context.Context, streams int, bytesPerStream int) (float64, error) {
	var mu sync.Mutex
	var totalBytes int64
	var wg sync.WaitGroup
	var firstErr error

	start := time.Now()

	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := make([]byte, bytesPerStream)
			rand.Read(data)

			pr, pw := io.Pipe()
			go func() {
				pw.Write(data)
				pw.Close()
			}()

			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://speed.cloudflare.com/__up", pr)
			req.ContentLength = int64(bytesPerStream)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			resp.Body.Close()

			mu.Lock()
			totalBytes += int64(bytesPerStream)
			mu.Unlock()
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	if totalBytes == 0 {
		if firstErr != nil {
			return 0, firstErr
		}
		return 0, fmt.Errorf("no data sent")
	}

	return float64(totalBytes*8) / elapsed.Seconds(), nil
}
