package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "valid IPv4 and hostname", config: Config{Targets: []string{"1.1.1.1", "example.com"}, ScheduleSeconds: 30, DelayMilliseconds: 10, TimeoutMilliseconds: 1000}},
		{name: "no targets", config: Config{ScheduleSeconds: 30, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "invalid target", config: Config{Targets: []string{"--help"}, ScheduleSeconds: 30, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "invalid hostname labels", config: Config{Targets: []string{"a..b"}, ScheduleSeconds: 30, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "schedule out of range", config: Config{Targets: []string{"localhost"}, ScheduleSeconds: 0, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "timeout out of range", config: Config{Targets: []string{"localhost"}, ScheduleSeconds: 1, TimeoutMilliseconds: 99}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateConfig(test.config)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateConfig() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestConfigAPIReadsAndPersistsConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "config.json")
	app, err := NewApp(path, func(context.Context, string, time.Duration) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}

	get := httptest.NewRecorder()
	app.routes().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", get.Code, http.StatusOK)
	}
	var defaults Config
	if err := json.Unmarshal(get.Body.Bytes(), &defaults); err != nil {
		t.Fatal(err)
	}
	if len(defaults.Targets) == 0 {
		t.Fatal("default configuration has no targets")
	}

	body := `{"targets":["localhost"],"schedule_seconds":15,"delay_milliseconds":25,"timeout_milliseconds":500}`
	put := httptest.NewRecorder()
	app.routes().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %q", put.Code, put.Body.String())
	}
	persisted, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Targets[0] != "localhost" || persisted.ScheduleSeconds != 15 ||
		persisted.DelayMilliseconds != 25 || persisted.TimeoutMilliseconds != 500 {
		t.Fatalf("persisted configuration = %+v", persisted)
	}
}

func TestConfigAPIRejectsInvalidAndUnknownFields(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"targets":["localhost"],"schedule_seconds":0,"delay_milliseconds":0,"timeout_milliseconds":1000}`,
		`{"targets":["localhost"],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"extra":true}`,
	} {
		response := httptest.NewRecorder()
		app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Errorf("PUT %s status = %d, want %d", body, response.Code, http.StatusBadRequest)
		}
	}
}

func TestMetricsReflectLatestProbeAndCounters(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(123, 0)
	app.mu.Lock()
	app.results["1.1.1.1"] = ProbeResult{Success: true, Duration: 12 * time.Millisecond, CheckedAt: now}
	app.counts["1.1.1.1:success"] = 4
	app.counts["1.1.1.1:failure"] = 2
	app.mu.Unlock()

	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, expected := range []string{
		`network_ping_success{target="1.1.1.1"} 1`,
		`network_ping_duration_seconds{target="1.1.1.1"} 0.012`,
		`network_ping_last_check_timestamp_seconds{target="1.1.1.1"} 123`,
		`network_ping_checks_total{target="1.1.1.1",result="success"} 4`,
		`network_ping_checks_total{target="1.1.1.1",result="failure"} 2`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("metrics missing %q:\n%s", expected, response.Body.String())
		}
	}
}

func TestWorkerUsesConfiguredTargetsAndTimeout(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.config = Config{
		Targets:             []string{"first.example", "second.example"},
		ScheduleSeconds:     60,
		DelayMilliseconds:   0,
		TimeoutMilliseconds: 750,
	}
	type observedProbe struct {
		target  string
		timeout time.Duration
	}
	observed := make(chan observedProbe, 2)
	app.probe = func(_ context.Context, target string, timeout time.Duration) ProbeResult {
		observed <- observedProbe{target: target, timeout: timeout}
		return ProbeResult{Success: true, Duration: time.Millisecond}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		app.run(ctx)
		close(done)
	}()
	for _, target := range []string{"first.example", "second.example"} {
		select {
		case result := <-observed:
			if result.target != target || result.timeout != 750*time.Millisecond {
				t.Fatalf("probe = %+v, expected %s with 750ms timeout", result, target)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for configured probe")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func TestWorkerHonorsDelayBetweenTargetsAndScheduleBetweenRounds(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.config = Config{
		Targets:             []string{"first.example", "second.example"},
		ScheduleSeconds:     1,
		DelayMilliseconds:   40,
		TimeoutMilliseconds: 750,
	}
	type observedProbe struct {
		target string
		at     time.Time
	}
	observed := make(chan observedProbe, 4)
	app.probe = func(_ context.Context, target string, _ time.Duration) ProbeResult {
		observed <- observedProbe{target: target, at: time.Now()}
		return ProbeResult{Success: true, Duration: time.Millisecond}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		app.run(ctx)
		close(done)
	}()
	probes := make([]observedProbe, 3)
	for i := range probes {
		select {
		case probes[i] = <-observed:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for scheduled probe")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}

	if probes[0].target != "first.example" || probes[1].target != "second.example" ||
		probes[2].target != "first.example" {
		t.Fatalf("probe order = %q, %q, %q", probes[0].target, probes[1].target, probes[2].target)
	}
	if delay := probes[1].at.Sub(probes[0].at); delay < 30*time.Millisecond {
		t.Errorf("delay between targets = %s, want at least 30ms", delay)
	}
	if interval := probes[2].at.Sub(probes[0].at); interval < 900*time.Millisecond {
		t.Errorf("interval between rounds = %s, want at least 900ms", interval)
	}
}

func TestIndexServesConfigurationPage(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Save configuration") {
		t.Fatalf("unexpected page response: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestLoadConfigRejectsCorruptSavedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig accepted malformed JSON")
	}
}
