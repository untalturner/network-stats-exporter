package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOoklaSpeedtestAcceptsLicenseAfterUserAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_FILE\"\nprintf '%s\\n' '{\"download\":{\"bandwidth\":1000},\"upload\":{\"bandwidth\":500},\"ping\":{\"latency\":10}}'\n"
	if err := os.WriteFile(filepath.Join(dir, "speedtest"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ARGS_FILE", argsPath)

	if _, err := runOoklaSpeedtest(context.Background()); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--accept-license") || !strings.Contains(string(args), "--accept-gdpr") ||
		!strings.Contains(string(args), "--format=json") {
		t.Fatalf("Ookla CLI arguments = %q; want license and GDPR acceptance flags and JSON output", args)
	}
}

func TestParseOoklaSpeedtestJSON(t *testing.T) {
	data := `{"type":"result","ping":{"latency":12.34,"jitter":1.2},"download":{"bandwidth":12500000},"upload":{"bandwidth":2500000},"packetLoss":0.5}`
	measurement, err := parseOoklaSpeedtestJSON([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if measurement.DownloadBitsPerSecond != 100_000_000 || measurement.UploadBitsPerSecond != 20_000_000 ||
		measurement.Latency != 12340*time.Microsecond || measurement.PacketLossPercent == nil || *measurement.PacketLossPercent != 0.5 {
		t.Fatalf("parsed Ookla measurement = %+v", measurement)
	}
}

func TestParseOoklaSpeedtestJSONRejectsInvalidResults(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed JSON", data: "{"},
		{name: "missing required measurement", data: `{"download":{"bandwidth":1},"upload":{"bandwidth":1}}`},
		{name: "negative throughput", data: `{"download":{"bandwidth":-1},"upload":{"bandwidth":1},"ping":{"latency":1}}`},
		{name: "invalid packet loss", data: `{"download":{"bandwidth":1},"upload":{"bandwidth":1},"ping":{"latency":1},"packetLoss":101}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseOoklaSpeedtestJSON([]byte(test.data)); err == nil {
				t.Fatalf("parseOoklaSpeedtestJSON() error = %v, want an error", err)
			}
		})
	}
}

func TestRunCloudflareSpeedtestAtMeasuresLatencyAndBandwidth(t *testing.T) {
	var latencyRequests, downloadRequests, uploadRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		byteCount, err := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
		if err != nil {
			http.Error(w, "missing bytes", http.StatusBadRequest)
			return
		}
		switch {
		case r.URL.Path == "/__down" && r.Method == http.MethodGet && byteCount == 0:
			latencyRequests.Add(1)
		case r.URL.Path == "/__down" && r.Method == http.MethodGet && byteCount == cloudflareTransferBytesPerMB:
			downloadRequests.Add(1)
			_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", int(byteCount))), byteCount)
		case r.URL.Path == "/__up" && r.Method == http.MethodPost && byteCount == cloudflareTransferBytesPerMB:
			uploaded, err := io.Copy(io.Discard, r.Body)
			if err != nil || uploaded != byteCount {
				http.Error(w, "invalid upload size", http.StatusBadRequest)
				return
			}
			uploadRequests.Add(1)
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	measurement, err := runCloudflareSpeedtestAt(context.Background(), server.Client(), server.URL, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.DownloadBitsPerSecond <= 0 || measurement.UploadBitsPerSecond <= 0 || measurement.Latency <= 0 {
		t.Fatalf("Cloudflare measurement = %+v; expected positive latency and throughput", measurement)
	}
	if measurement.PacketLossPercent != nil {
		t.Fatalf("Cloudflare packet loss = %v; packet loss is not measured by this provider", measurement.PacketLossPercent)
	}
	if latencyRequests.Load() != cloudflareLatencySampleCount ||
		downloadRequests.Load() != 2 ||
		uploadRequests.Load() != 2 {
		t.Fatalf("request counts = latency %d, download %d, upload %d",
			latencyRequests.Load(), downloadRequests.Load(), uploadRequests.Load())
	}
}

func TestRunCloudflareSpeedtestAtReportsHTTPFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	_, err := runCloudflareSpeedtestAt(context.Background(), server.Client(), server.URL, 4, 5)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("Cloudflare speedtest error = %v; want HTTP 502 failure", err)
	}
}

func TestRunCloudflareSpeedtestAtHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bytes") == "0" {
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := runCloudflareSpeedtestAt(ctx, server.Client(), server.URL, 4, 5)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cloudflare cancellation error = %v; want context deadline exceeded", err)
	}
}

func TestRunCloudflareSpeedtestAtRejectsUnsafeSettings(t *testing.T) {
	for _, test := range []struct {
		name        string
		concurrency int
		sizeMB      int
	}{
		{name: "zero concurrency", concurrency: 0, sizeMB: 1},
		{name: "too many concurrent requests", concurrency: 9, sizeMB: 1},
		{name: "zero request size", concurrency: 1, sizeMB: 0},
		{name: "too large request", concurrency: 1, sizeMB: 51},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := runCloudflareSpeedtestAt(context.Background(), http.DefaultClient, cloudflareSpeedtestURL, test.concurrency, test.sizeMB)
			if err == nil {
				t.Fatal("Cloudflare speedtest accepted settings outside the supported bounds")
			}
		})
	}
}
