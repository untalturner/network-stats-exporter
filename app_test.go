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
		{name: "valid IPv4 and hostname", config: Config{Targets: []Target{{Address: "1.1.1.1", Type: "external"}, {Address: "example.com", Type: "local"}}, ScheduleSeconds: 30, DelayMilliseconds: 10, TimeoutMilliseconds: 1000}},
		{name: "no targets", config: Config{ScheduleSeconds: 30, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "invalid target", config: Config{Targets: []Target{{Address: "--help", Type: "local"}}, ScheduleSeconds: 30, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "invalid hostname labels", config: Config{Targets: []Target{{Address: "a..b", Type: "local"}}, ScheduleSeconds: 30, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "invalid target type", config: Config{Targets: []Target{{Address: "localhost", Type: "lan network"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "missing target type", config: Config{Targets: []Target{{Address: "localhost"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "schedule out of range", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 0, TimeoutMilliseconds: 1000}, wantErr: true},
		{name: "timeout out of range", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 99}, wantErr: true},
		{name: "packet count out of range", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, PacketsPerPing: -1}, wantErr: true},
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
	app, err := NewApp(path, func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
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
	if defaults.ScheduleSeconds != 1 || defaults.DelayMilliseconds != 0 {
		t.Fatalf("default probe cadence = schedule %ds, delay %dms; want 1s and 0ms",
			defaults.ScheduleSeconds, defaults.DelayMilliseconds)
	}
	if defaults.PacketsPerPing != 5 {
		t.Fatalf("default packets per ping = %d, want 5", defaults.PacketsPerPing)
	}

	body := `{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":15,"delay_milliseconds":25,"timeout_milliseconds":500,"packets_per_ping":7}`
	put := httptest.NewRecorder()
	app.routes().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %q", put.Code, put.Body.String())
	}
	persisted, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Targets[0] != (Target{Address: "localhost", Type: "local"}) || persisted.ScheduleSeconds != 15 ||
		persisted.DelayMilliseconds != 25 || persisted.TimeoutMilliseconds != 500 || persisted.PacketsPerPing != 7 {
		t.Fatalf("persisted configuration = %+v", persisted)
	}
}

func TestConfigResetRestoresAndPersistsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	app, err := NewApp(path, func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}

	body := `{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":15,"delay_milliseconds":25,"timeout_milliseconds":500,"packets_per_ping":7}`
	put := httptest.NewRecorder()
	app.routes().ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %q", put.Code, put.Body.String())
	}

	reset := httptest.NewRecorder()
	app.routes().ServeHTTP(reset, httptest.NewRequest(http.MethodPost, "/api/config/reset", nil))
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status = %d, body = %q", reset.Code, reset.Body.String())
	}
	var response Config
	if err := json.Unmarshal(reset.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := defaultConfig()
	if len(response.Targets) != len(want.Targets) || response.Targets[0] != want.Targets[0] ||
		response.ScheduleSeconds != want.ScheduleSeconds || response.DelayMilliseconds != want.DelayMilliseconds ||
		response.TimeoutMilliseconds != want.TimeoutMilliseconds || response.PacketsPerPing != want.PacketsPerPing {
		t.Fatalf("reset response = %+v, want defaults %+v", response, want)
	}
	persisted, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Targets[0] != want.Targets[0] || persisted.ScheduleSeconds != want.ScheduleSeconds {
		t.Fatalf("persisted reset configuration = %+v, want defaults %+v", persisted, want)
	}
}

func TestConfigSaveReturnsFilesystemError(t *testing.T) {
	dir := t.TempDir()
	blockedDir := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blockedDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{
		configPath: filepath.Join(blockedDir, "config.json"),
		config:     defaultConfig(),
	}
	body := `{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":15,"delay_milliseconds":25,"timeout_milliseconds":500,"packets_per_ping":7}`
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	if response.Code != http.StatusInternalServerError ||
		!strings.Contains(response.Body.String(), "save configuration to") ||
		!strings.Contains(response.Body.String(), "writable by the exporter") {
		t.Fatalf("save failure response = status %d, body %q", response.Code, response.Body.String())
	}
	if app.config.ScheduleSeconds != defaultConfig().ScheduleSeconds {
		t.Fatal("in-memory config changed after persistence failed")
	}
}

func TestLoadConfigDefaultsPacketsForOlderConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"targets":["localhost"],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"prometheus_url":"http://old-host:9090","grafana_url":"http://old-host:3000"}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.PacketsPerPing != 5 || config.Targets[0].Type != "default" {
		t.Fatalf("legacy config migrated to packets=%d, target type=%q; want 5 and default", config.PacketsPerPing, config.Targets[0].Type)
	}
}

func TestConfigAPIRejectsInvalidAndUnknownFields(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"targets":["localhost"],"schedule_seconds":0,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5}`,
		`{"targets":["localhost"],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":21}`,
		`{"targets":["localhost"],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5,"extra":true}`,
		`{"targets":[{"address":"localhost","type":"local","extra":true}],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5}`,
		`{"targets":[{"address":"localhost","type":"bad type"}],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5}`,
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
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	app.config.Targets = []Target{{Address: "1.1.1.1", Type: "external"}}
	app.results["1.1.1.1"] = ProbeResult{
		Success: true, Duration: 12 * time.Millisecond, PacketsSent: 2, PacketsReceived: 1,
	}
	app.mu.Unlock()

	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, expected := range []string{
		`network_ping_success{target="1.1.1.1",type="external"} 1`,
		`network_ping_duration_seconds{target="1.1.1.1",type="external"} 0.012`,
		`network_ping_packet_loss_ratio{target="1.1.1.1",type="external"} 0.5`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("metrics missing %q:\n%s", expected, response.Body.String())
		}
	}

	app.mu.Lock()
	app.results["1.1.1.1"] = ProbeResult{PacketsSent: 1}
	app.mu.Unlock()
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(response.Body.String(), `network_ping_duration_seconds{target="1.1.1.1",type="external"}`) {
		t.Error("latency metric should be omitted when the latest ping failed")
	}
	if !strings.Contains(response.Body.String(), `network_ping_packet_loss_ratio{target="1.1.1.1",type="external"} 1`) {
		t.Error("packet loss should reflect the latest failed ping, not prior successful pings")
	}
	if strings.Contains(response.Body.String(), "_total") {
		t.Error("metrics should not expose cumulative counters")
	}
}

func TestWorkerUsesConfiguredTargetsAndTimeout(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.config = Config{
		Targets:             []Target{{Address: "first.example", Type: "local"}, {Address: "second.example", Type: "external"}},
		ScheduleSeconds:     60,
		DelayMilliseconds:   0,
		TimeoutMilliseconds: 750,
		PacketsPerPing:      5,
	}
	type observedProbe struct {
		target  string
		timeout time.Duration
		count   int
	}
	observed := make(chan observedProbe, 2)
	app.probe = func(_ context.Context, target string, timeout time.Duration, count int) ProbeResult {
		observed <- observedProbe{target: target, timeout: timeout, count: count}
		return ProbeResult{Success: true, Duration: time.Millisecond, PacketsSent: uint64(count), PacketsReceived: uint64(count)}
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
			if result.target != target || result.timeout != 750*time.Millisecond || result.count != 5 {
				t.Fatalf("probe = %+v, expected %s with 750ms timeout and 5 packets", result, target)
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
	for _, target := range []string{"first.example", "second.example"} {
		result := app.results[target]
		if result.PacketsSent != 5 || result.PacketsReceived != 5 {
			t.Errorf("%s latest packet stats = sent %d received %d, want 5 each",
				target, result.PacketsSent, result.PacketsReceived)
		}
	}
}

func TestWorkerHonorsDelayBetweenTargetsAndScheduleBetweenRounds(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.config = Config{
		Targets:             []Target{{Address: "first.example", Type: "local"}, {Address: "second.example", Type: "external"}},
		ScheduleSeconds:     1,
		DelayMilliseconds:   40,
		TimeoutMilliseconds: 750,
		PacketsPerPing:      5,
	}
	type observedProbe struct {
		target string
		at     time.Time
	}
	observed := make(chan observedProbe, 4)
	app.probe = func(_ context.Context, target string, _ time.Duration, count int) ProbeResult {
		observed <- observedProbe{target: target, at: time.Now()}
		return ProbeResult{Success: true, Duration: time.Millisecond, PacketsSent: uint64(count), PacketsReceived: uint64(count)}
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
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Save configuration") ||
		!strings.Contains(response.Body.String(), "packets_per_ping") ||
		!strings.Contains(response.Body.String(), "Reset to defaults") ||
		!strings.Contains(response.Body.String(), "Add target") ||
		!strings.Contains(response.Body.String(), "remove-target") ||
		strings.Contains(response.Body.String(), "prometheus_url") ||
		strings.Contains(response.Body.String(), "grafana_url") {
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
