package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPingHostParsesSuccessfulPing(t *testing.T) {
	dir := t.TempDir()
	pingPath := filepath.Join(dir, "ping")
	script := "#!/bin/sh\nprintf '%s\\n' '64 bytes from test: icmp_seq=1 ttl=64 time=1.25 ms'\n"
	if err := os.WriteFile(pingPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	result := pingHost(context.Background(), "test.example", time.Second)
	if !result.Success {
		t.Fatal("pingHost reported failure for successful ping command")
	}
	if result.Duration != 1250*time.Microsecond {
		t.Fatalf("duration = %s, want 1.25ms", result.Duration)
	}
}

func TestPingHostReportsCommandFailure(t *testing.T) {
	dir := t.TempDir()
	pingPath := filepath.Join(dir, "ping")
	if err := os.WriteFile(pingPath, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	result := pingHost(context.Background(), "test.example", time.Second)
	if result.Success {
		t.Fatal("pingHost reported success for failed ping command")
	}
}
