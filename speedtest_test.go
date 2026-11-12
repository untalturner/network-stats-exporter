package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
