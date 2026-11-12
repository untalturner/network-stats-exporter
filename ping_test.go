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
	script := "#!/bin/sh\nprintf '%s\\n' '64 bytes from test: icmp_seq=1 ttl=64 time=1.25 ms' '64 bytes from test: icmp_seq=2 ttl=64 time=2.25 ms' '64 bytes from test: icmp_seq=3 ttl=64 time=0.25 ms' '3 packets transmitted, 3 received, 0% packet loss, time 0ms'\n"
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
	if !result.JitterAvailable || result.Jitter != 1500*time.Microsecond {
		t.Fatalf("jitter = %s (available %t), want 1.5ms", result.Jitter, result.JitterAvailable)
	}
	if result.PacketsSent != 3 || result.PacketsReceived != 3 {
		t.Fatalf("packet counts = sent %d received %d, want 3 and 3", result.PacketsSent, result.PacketsReceived)
	}
}

func TestParsePingRTTsAndCalculateJitter(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		want      time.Duration
		available bool
	}{
		{
			name:      "varying consecutive replies",
			output:    "reply icmp_seq=1 time=10 ms\nreply icmp_seq=2 time=20 ms\nreply icmp_seq=3 time=15 ms",
			want:      7500 * time.Microsecond,
			available: true,
		},
		{
			name:      "stable replies",
			output:    "reply icmp_seq=1 time=10 ms\nreply icmp_seq=2 time=10 ms\nreply icmp_seq=3 time=10 ms",
			want:      0,
			available: true,
		},
		{
			name:      "does not bridge a missing sequence",
			output:    "reply icmp_seq=1 time=10 ms\nreply icmp_seq=2 time=20 ms\nreply icmp_seq=4 time=40 ms\nreply icmp_seq=5 time=60 ms",
			want:      15 * time.Millisecond,
			available: true,
		},
		{
			name:      "windows timeout separates replies",
			output:    "Reply from 192.0.2.1: time=10ms\nRequest timed out.\nReply from 192.0.2.1: time=40ms",
			available: false,
		},
		{
			name:      "one reply has no jitter",
			output:    "reply icmp_seq=1 time=10 ms",
			available: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, available := calculatePingJitter(parsePingRTTs([]byte(test.output)))
			if got != test.want || available != test.available {
				t.Fatalf("jitter = %s (available %t), want %s (available %t)", got, available, test.want, test.available)
			}
		})
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
