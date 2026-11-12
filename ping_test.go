package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPingHostParsesSuccessfulPing(t *testing.T) {
	dir := t.TempDir()
	pingPath := filepath.Join(dir, "ping")
	script := "#!/bin/sh\nprintf '%s\\n' '64 bytes from test: icmp_seq=1 ttl=64 time=1.25 ms' '3 packets transmitted, 3 received, 0% packet loss, time 0ms'\n"
	if err := os.WriteFile(pingPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	result := pingHost(context.Background(), "test.example", time.Second, 3)
	if !result.Success {
		t.Fatal("pingHost reported failure for successful ping command")
	}
	if result.Duration != 1250*time.Microsecond {
		t.Fatalf("duration = %s, want 1.25ms", result.Duration)
	}
	if result.PacketsSent != 3 || result.PacketsReceived != 3 {
		t.Fatalf("packet counts = sent %d received %d, want 3 and 3", result.PacketsSent, result.PacketsReceived)
	}
}

func TestPingHostReportsCommandFailure(t *testing.T) {
	dir := t.TempDir()
	pingPath := filepath.Join(dir, "ping")
	script := "#!/bin/sh\nprintf '%s\\n' '1 packets transmitted, 0 received, 100% packet loss, time 0ms'\nexit 1\n"
	if err := os.WriteFile(pingPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	result := pingHost(context.Background(), "test.example", time.Second, 1)
	if result.Success {
		t.Fatal("pingHost reported success for failed ping command")
	}
	if result.PacketsSent != 1 || result.PacketsReceived != 0 {
		t.Fatalf("packet counts = sent %d received %d, want 1 and 0", result.PacketsSent, result.PacketsReceived)
	}
}

func TestPingArgsUseConfiguredPacketCount(t *testing.T) {
	tests := []struct {
		goos string
		want []string
	}{
		{goos: "linux", want: []string{"-n", "-c", "5", "-i", "0.2", "-W", "1.000", "example.com"}},
		{goos: "darwin", want: []string{"-n", "-c", "5", "-i", "0.2", "-W", "1000", "example.com"}},
		{goos: "windows", want: []string{"-n", "5", "-w", "1000", "example.com"}},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			got := pingArgs(test.goos, "example.com", time.Second, 5)
			if strings.Join(got, " ") != strings.Join(test.want, " ") {
				t.Fatalf("ping args = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParsePingPacketStatistics(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		sent     uint64
		received uint64
	}{
		{
			name:     "Linux iputils",
			output:   "1 packets transmitted, 0 received, 100% packet loss, time 0ms",
			sent:     1,
			received: 0,
		},
		{
			name:     "macOS",
			output:   "2 packets transmitted, 1 packets received, 50.0% packet loss",
			sent:     2,
			received: 1,
		},
		{
			name:     "Windows",
			output:   "Packets: Sent = 4, Received = 3, Lost = 1 (25% loss)",
			sent:     4,
			received: 3,
		},
		{name: "missing summary"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sent, received := parsePingPacketStatistics([]byte(test.output))
			if sent != test.sent || received != test.received {
				t.Fatalf("parsed packets = sent %d received %d, want sent %d received %d",
					sent, received, test.sent, test.received)
			}
		})
	}
}
