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
		{name: "speedtest interval out of range", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestIntervalSeconds: 299}, wantErr: true},
		{name: "speedtest timeout out of range", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestTimeoutSeconds: 29}, wantErr: true},
		{name: "automatic speedtest without terms", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestEnabled: true}, wantErr: true},
		{name: "automatic Ookla speedtest without terms", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestOoklaEnabled: true}, wantErr: true},
		{name: "automatic Cloudflare speedtest without notice", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestCloudflareEnabled: true}, wantErr: true},
		{name: "unsupported speedtest provider", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestProvider: "other"}, wantErr: true},
		{name: "automatic Cloudflare speedtest without notice", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestEnabled: true, SpeedtestProvider: speedtestProviderCloudflare}, wantErr: true},
		{name: "automatic Cloudflare speedtest with notice", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, SpeedtestEnabled: true, SpeedtestProvider: speedtestProviderCloudflare, SpeedtestCloudflareNoticeAccepted: true}},
		{name: "Cloudflare request concurrency too high", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, CloudflareConcurrentRequests: 9}, wantErr: true},
		{name: "Cloudflare request size too high", config: Config{Targets: []Target{{Address: "localhost", Type: "local"}}, ScheduleSeconds: 1, TimeoutMilliseconds: 1000, CloudflareRequestSizeMB: 51}, wantErr: true},
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

func TestValidateConfigCronSchedules(t *testing.T) {
	tests := []struct {
		name           string
		ooklaCron      string
		cloudflareCron string
		wantErr        bool
	}{
		{name: "both provider cron schedules", ooklaCron: "0 */6 * * *", cloudflareCron: "15 */6 * * *"},
		{name: "invalid Ookla cron schedule", ooklaCron: "every day", wantErr: true},
		{name: "invalid Cloudflare cron schedule", cloudflareCron: "60 * * * *", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := defaultConfig()
			config.SpeedtestOoklaCron = test.ooklaCron
			config.SpeedtestCloudflareCron = test.cloudflareCron
			if _, err := validateConfig(config); (err != nil) != test.wantErr {
				t.Fatalf("validateConfig() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestLoadConfigMigratesLegacySpeedtestSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5,"speedtest_enabled":true,"speedtest_provider":"cloudflare","speedtest_cloudflare_notice_accepted":true,"speedtest_interval_seconds":3600,"speedtest_timeout_seconds":420,"cloudflare_concurrent_requests":6,"cloudflare_request_size_mb":12}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.SpeedtestEnabled || !config.SpeedtestCloudflareEnabled || config.SpeedtestOoklaEnabled ||
		config.SpeedtestCloudflareIntervalSeconds != 3600 || config.SpeedtestCloudflareCron != "" ||
		config.SpeedtestTimeoutSeconds != 420 || config.CloudflareConcurrentRequests != 6 {
		t.Fatalf("legacy speedtest config migration = %+v", config)
	}
}

func TestCalculateNextSpeedtestRunSupportsIndependentIntervalsAndCron(t *testing.T) {
	now := time.Date(2026, time.November, 12, 1, 7, 0, 0, time.UTC)
	config := defaultConfig()
	config.SpeedtestTermsAccepted = true
	config.SpeedtestCloudflareNoticeAccepted = true
	config.SpeedtestOoklaEnabled = true
	config.SpeedtestOoklaCron = "*/15 * * * *"
	config.SpeedtestCloudflareEnabled = true
	config.SpeedtestCloudflareIntervalSeconds = 600

	ooklaNext := calculateNextSpeedtestRun(config, speedtestProviderOokla, now)
	if want := time.Date(2026, time.November, 12, 1, 15, 0, 0, time.UTC); !ooklaNext.Equal(want) {
		t.Errorf("Ookla next cron run = %s, want %s", ooklaNext, want)
	}
	cloudflareNext := calculateNextSpeedtestRun(config, speedtestProviderCloudflare, now)
	if want := now.Add(10 * time.Minute); !cloudflareNext.Equal(want) {
		t.Errorf("Cloudflare next interval run = %s, want %s", cloudflareNext, want)
	}

	config.SpeedtestCloudflareEnabled = false
	if next := calculateNextSpeedtestRun(config, speedtestProviderCloudflare, now); !next.IsZero() {
		t.Errorf("disabled Cloudflare schedule next run = %s, want zero time", next)
	}
}

func TestSpeedtestStatusIncludesNextRunTimestamp(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, time.November, 12, 2, 0, 0, 0, time.UTC)
	app.speedtestStatus[speedtestProviderOokla] = SpeedtestStatus{NextRunAt: &next}
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/speedtest", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"next_run_at":"2026-11-12T02:00:00Z"`) {
		t.Fatalf("speedtest status with next run = %d, %s", response.Code, response.Body.String())
	}
}

func TestSpeedtestsRunIndependentlyAndExposeProviderLabels(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.config.SpeedtestTermsAccepted = true
	app.config.SpeedtestCloudflareNoticeAccepted = true
	started := make(chan string, 2)
	release := make(chan struct{})
	app.speedtest = func(context.Context) (SpeedtestMeasurement, error) {
		started <- speedtestProviderOokla
		<-release
		return SpeedtestMeasurement{DownloadBitsPerSecond: 100}, nil
	}
	app.cloudflareSpeedtest = func(context.Context, int, int) (SpeedtestMeasurement, error) {
		started <- speedtestProviderCloudflare
		<-release
		return SpeedtestMeasurement{DownloadBitsPerSecond: 200}, nil
	}
	for _, provider := range []string{speedtestProviderOokla, speedtestProviderCloudflare} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/speedtest/run?provider="+provider, nil)
		app.routes().ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s run status = %d, body = %q", provider, response.Code, response.Body.String())
		}
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("both providers did not start concurrently")
		}
	}
	app.mu.RLock()
	ooklaRunning := app.speedtestStatus[speedtestProviderOokla].Running
	cloudflareRunning := app.speedtestStatus[speedtestProviderCloudflare].Running
	app.mu.RUnlock()
	if !ooklaRunning || !cloudflareRunning {
		t.Fatalf("running states: Ookla=%t Cloudflare=%t", ooklaRunning, cloudflareRunning)
	}
	metrics := httptest.NewRecorder()
	app.routes().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, provider := range []string{speedtestProviderOokla, speedtestProviderCloudflare} {
		if !strings.Contains(metrics.Body.String(), `network_speedtest_running{provider="`+provider+`"} 1`) {
			t.Errorf("running metric for %s missing:\n%s", provider, metrics.Body.String())
		}
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		app.mu.RLock()
		finished := app.speedtestStatus[speedtestProviderOokla].Attempted && app.speedtestStatus[speedtestProviderCloudflare].Attempted
		app.mu.RUnlock()
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("both provider tests did not finish")
		}
		time.Sleep(time.Millisecond)
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
	if defaults.SpeedtestEnabled || defaults.SpeedtestOoklaEnabled || defaults.SpeedtestCloudflareEnabled ||
		defaults.SpeedtestOoklaIntervalSeconds != 1800 || defaults.SpeedtestCloudflareIntervalSeconds != 1800 ||
		defaults.SpeedtestProvider != speedtestProviderOokla ||
		defaults.SpeedtestIntervalSeconds != 1800 || defaults.SpeedtestTimeoutSeconds != 300 ||
		defaults.SpeedtestTermsAccepted || defaults.SpeedtestCloudflareNoticeAccepted ||
		defaults.CloudflareConcurrentRequests != 4 || defaults.CloudflareRequestSizeMB != 5 {
		t.Fatalf("default speedtest config = %+v; want disabled Ookla, 1800s, 300s, 4x5MB, and unaccepted notices", defaults)
	}

	body := `{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":15,"delay_milliseconds":25,"timeout_milliseconds":500,"packets_per_ping":7,"speedtest_enabled":false,"speedtest_provider":"ookla","speedtest_terms_accepted":true,"speedtest_cloudflare_notice_accepted":true,"speedtest_interval_seconds":3600,"speedtest_timeout_seconds":420,"speedtest_ookla_enabled":true,"speedtest_ookla_interval_seconds":1800,"speedtest_ookla_cron":"0 */6 * * *","speedtest_cloudflare_enabled":true,"speedtest_cloudflare_interval_seconds":7200,"speedtest_cloudflare_cron":"15 */6 * * *","cloudflare_concurrent_requests":6,"cloudflare_request_size_mb":12}`
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
		persisted.DelayMilliseconds != 25 || persisted.TimeoutMilliseconds != 500 || persisted.PacketsPerPing != 7 ||
		persisted.SpeedtestEnabled || persisted.SpeedtestProvider != speedtestProviderOokla ||
		!persisted.SpeedtestTermsAccepted || !persisted.SpeedtestCloudflareNoticeAccepted ||
		persisted.SpeedtestIntervalSeconds != 3600 ||
		!persisted.SpeedtestOoklaEnabled || persisted.SpeedtestOoklaIntervalSeconds != 1800 ||
		persisted.SpeedtestOoklaCron != "0 */6 * * *" ||
		!persisted.SpeedtestCloudflareEnabled || persisted.SpeedtestCloudflareIntervalSeconds != 7200 ||
		persisted.SpeedtestCloudflareCron != "15 */6 * * *" ||
		persisted.SpeedtestTimeoutSeconds != 420 || persisted.CloudflareConcurrentRequests != 6 ||
		persisted.CloudflareRequestSizeMB != 12 {
		t.Fatalf("persisted configuration = %+v", persisted)
	}
}

func TestConfigResetRestoresAndPersistsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	app, err := NewApp(path, func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}

	body := `{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":15,"delay_milliseconds":25,"timeout_milliseconds":500,"packets_per_ping":7,"speedtest_enabled":true,"speedtest_terms_accepted":true,"speedtest_interval_seconds":3600,"speedtest_timeout_seconds":420}`
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
		response.TimeoutMilliseconds != want.TimeoutMilliseconds || response.PacketsPerPing != want.PacketsPerPing ||
		response.SpeedtestEnabled != want.SpeedtestEnabled || response.SpeedtestTermsAccepted != want.SpeedtestTermsAccepted ||
		response.SpeedtestProvider != want.SpeedtestProvider ||
		response.SpeedtestCloudflareNoticeAccepted != want.SpeedtestCloudflareNoticeAccepted ||
		response.SpeedtestIntervalSeconds != want.SpeedtestIntervalSeconds || response.SpeedtestTimeoutSeconds != want.SpeedtestTimeoutSeconds ||
		response.CloudflareConcurrentRequests != want.CloudflareConcurrentRequests ||
		response.CloudflareRequestSizeMB != want.CloudflareRequestSizeMB {
		t.Fatalf("reset response = %+v, want defaults %+v", response, want)
	}
	persisted, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Targets[0] != want.Targets[0] || persisted.ScheduleSeconds != want.ScheduleSeconds ||
		persisted.SpeedtestIntervalSeconds != want.SpeedtestIntervalSeconds ||
		persisted.SpeedtestTimeoutSeconds != want.SpeedtestTimeoutSeconds ||
		persisted.CloudflareConcurrentRequests != want.CloudflareConcurrentRequests ||
		persisted.CloudflareRequestSizeMB != want.CloudflareRequestSizeMB {
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
	if config.SpeedtestEnabled || config.SpeedtestProvider != speedtestProviderOokla ||
		config.SpeedtestTermsAccepted || config.SpeedtestCloudflareNoticeAccepted ||
		config.SpeedtestIntervalSeconds != 1800 || config.SpeedtestTimeoutSeconds != 300 {
		t.Fatalf("legacy speedtest config migrated to %+v; want disabled Ookla, 30-minute interval, and 5-minute timeout", config)
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
		`{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5,"speedtest_interval_seconds":299}`,
		`{"targets":[{"address":"localhost","type":"local"}],"schedule_seconds":1,"delay_milliseconds":0,"timeout_milliseconds":1000,"packets_per_ping":5,"speedtest_enabled":true}`,
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
		Success: true, Duration: 12 * time.Millisecond, Jitter: 4 * time.Millisecond,
		JitterAvailable: true, PacketsSent: 2, PacketsReceived: 1,
	}
	app.mu.Unlock()

	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, expected := range []string{
		`network_ping_success{target="1.1.1.1",type="external"} 1`,
		`network_ping_duration_seconds{target="1.1.1.1",type="external"} 0.012`,
		`network_ping_jitter_seconds{target="1.1.1.1",type="external"} 0.004`,
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
	if strings.Contains(response.Body.String(), `network_ping_jitter_seconds{target="1.1.1.1",type="external"}`) {
		t.Error("jitter metric should be omitted when there are not two consecutive replies")
	}
	if !strings.Contains(response.Body.String(), `network_ping_packet_loss_ratio{target="1.1.1.1",type="external"} 1`) {
		t.Error("packet loss should reflect the latest failed ping, not prior successful pings")
	}
	if strings.Contains(response.Body.String(), "_total") {
		t.Error("metrics should not expose cumulative counters")
	}
}

func TestSpeedtestAPIStartsAndReportsManualRun(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	app.mu.Lock()
	app.config.SpeedtestTimeoutSeconds = 77
	app.mu.Unlock()
	observedTimeout := make(chan time.Duration, 1)
	packetLoss := 0.5
	app.speedtest = func(ctx context.Context) (SpeedtestMeasurement, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return SpeedtestMeasurement{}, context.DeadlineExceeded
		}
		observedTimeout <- time.Until(deadline)
		started <- struct{}{}
		return SpeedtestMeasurement{
			DownloadBitsPerSecond: 100_000_000,
			UploadBitsPerSecond:   20_000_000,
			Latency:               12 * time.Millisecond,
			PacketLossPercent:     &packetLoss,
		}, nil
	}

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := httptest.NewRecorder()
		app.routes().ServeHTTP(response, httptest.NewRequest(method, "/api/speedtest/run", nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("run endpoint %s status = %d, want %d", method, response.Code, http.StatusMethodNotAllowed)
		}
	}
	blocked := httptest.NewRecorder()
	app.routes().ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/api/speedtest/run", nil))
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("run without term acknowledgement status = %d, want %d", blocked.Code, http.StatusForbidden)
	}

	app.mu.Lock()
	app.config.SpeedtestTermsAccepted = true
	app.mu.Unlock()
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/speedtest/run", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, body = %q", response.Code, response.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("speedtest was not started")
	}
	select {
	case timeout := <-observedTimeout:
		if timeout < 76*time.Second || timeout > 77*time.Second {
			t.Errorf("speedtest timeout = %s, want 77 seconds", timeout)
		}
	case <-time.After(time.Second):
		t.Fatal("speedtest did not receive configured timeout")
	}
	deadline := time.Now().Add(time.Second)
	for {
		app.mu.RLock()
		finished := !app.speedtestStatus[speedtestProviderOokla].Running && app.speedtestStatus[speedtestProviderOokla].Attempted
		app.mu.RUnlock()
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("speedtest did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	statusResponse := httptest.NewRecorder()
	app.routes().ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/speedtest", nil))
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"success":true`) ||
		!strings.Contains(statusResponse.Body.String(), `"download_bits_per_second":100000000`) {
		t.Fatalf("speedtest status response = %d, %s", statusResponse.Code, statusResponse.Body.String())
	}
	metrics := httptest.NewRecorder()
	app.routes().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, expected := range []string{
		`network_speedtest_success{provider="ookla"} 1`,
		`network_speedtest_download_bits_per_second{provider="ookla"} 1e+08`,
		`network_speedtest_upload_bits_per_second{provider="ookla"} 2e+07`,
		`network_speedtest_latency_seconds{provider="ookla"} 0.012`,
		`network_speedtest_packet_loss_ratio{provider="ookla"} 0.005`,
		`network_speedtest_running{provider="ookla"} 0`,
		`network_speedtest_provider_info{provider="ookla"} 1`,
	} {
		if !strings.Contains(metrics.Body.String(), expected) {
			t.Errorf("speedtest metrics missing %q:\n%s", expected, metrics.Body.String())
		}
	}
}

func TestFailedSpeedtestMetricsOmitPreviousMeasurements(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	completedAt := time.Now().UTC()
	app.speedtestStatus[speedtestProviderOokla] = SpeedtestStatus{Attempted: true, LastRunAt: &completedAt, Error: "test failed"}
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, expected := range []string{
		`network_speedtest_success{provider="ookla"} 0`,
		`network_speedtest_last_run_timestamp_seconds{provider="ookla"} `,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("failed speedtest metrics missing %q:\n%s", expected, response.Body.String())
		}
	}
	for _, stale := range []string{
		"\nnetwork_speedtest_download_bits_per_second{provider=\"ookla\"} ",
		"\nnetwork_speedtest_upload_bits_per_second{provider=\"ookla\"} ",
		"\nnetwork_speedtest_latency_seconds{provider=\"ookla\"} ",
	} {
		if strings.Contains(response.Body.String(), stale) {
			t.Errorf("failed speedtest exposed stale measurement %q:\n%s", stale, response.Body.String())
		}
	}
}

func TestScheduledSpeedtestRunsAtConfiguredInterval(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}

	app.config.SpeedtestEnabled = true
	app.config.SpeedtestTermsAccepted = true
	app.config.SpeedtestIntervalSeconds = 1
	runs := make(chan struct{}, 1)
	app.speedtest = func(context.Context) (SpeedtestMeasurement, error) {
		runs <- struct{}{}
		return SpeedtestMeasurement{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		app.runScheduledSpeedtests(ctx)
		close(done)
	}()
	select {
	case <-runs:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("scheduled speedtest did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("speedtest scheduler did not stop after cancellation")
	}
}

func TestCloudflareSpeedtestRequiresNoticeAndUsesSelectedProvider(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"),
		func(context.Context, string, time.Duration, int) ProbeResult { return ProbeResult{} })
	if err != nil {
		t.Fatal(err)
	}
	app.config.SpeedtestProvider = speedtestProviderCloudflare
	app.config.CloudflareConcurrentRequests = 2
	app.config.CloudflareRequestSizeMB = 3
	started := make(chan struct{}, 1)
	var observedConcurrency, observedSize int
	app.cloudflareSpeedtest = func(_ context.Context, concurrency int, sizeMB int) (SpeedtestMeasurement, error) {
		observedConcurrency = concurrency
		observedSize = sizeMB
		started <- struct{}{}
		return SpeedtestMeasurement{DownloadBitsPerSecond: 50_000_000, UploadBitsPerSecond: 25_000_000, Latency: 20 * time.Millisecond}, nil
	}
	blocked := httptest.NewRecorder()
	app.routes().ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/api/speedtest/run", nil))
	if blocked.Code != http.StatusForbidden || !strings.Contains(blocked.Body.String(), "Cloudflare") {
		t.Fatalf("unacknowledged Cloudflare run = %d, %q", blocked.Code, blocked.Body.String())
	}

	app.config.SpeedtestCloudflareNoticeAccepted = true
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/speedtest/run", nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("Cloudflare run status = %d, body = %q", response.Code, response.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Cloudflare speedtest runner was not called")
	}
	deadline := time.Now().Add(time.Second)
	for {
		app.mu.RLock()
		finished := !app.speedtestStatus[speedtestProviderCloudflare].Running && app.speedtestStatus[speedtestProviderCloudflare].Attempted
		app.mu.RUnlock()
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Cloudflare speedtest did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.RLock()
	status := app.speedtestStatus[speedtestProviderCloudflare]
	app.mu.RUnlock()
	if status.Provider != speedtestProviderCloudflare || !status.Success {
		t.Fatalf("Cloudflare speedtest status = %+v", status)
	}
	if observedConcurrency != 2 || observedSize != 3 {
		t.Fatalf("Cloudflare parameters = concurrency %d, size %d MB; want 2 and 3", observedConcurrency, observedSize)
	}
	metrics := httptest.NewRecorder()
	app.routes().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), `network_speedtest_provider_info{provider="cloudflare"} 1`) {
		t.Fatalf("metrics do not identify Cloudflare provider:\n%s", metrics.Body.String())
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
	body := response.Body.String()
	pingSectionEnd := strings.Index(body, "Save ping configuration")
	latestPingSection := strings.Index(body, "Latest ping checks")
	speedtestSection := strings.Index(body, "Speedtests")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Save ping configuration") ||
		!strings.Contains(response.Body.String(), "Save Ookla settings") ||
		!strings.Contains(response.Body.String(), "Save Cloudflare settings") ||
		!strings.Contains(response.Body.String(), `id="ping-config"`) ||
		!strings.Contains(response.Body.String(), `id="ookla-config"`) ||
		!strings.Contains(response.Body.String(), `id="cloudflare-config"`) ||
		!strings.Contains(response.Body.String(), "packets_per_ping") ||
		!strings.Contains(response.Body.String(), "Reset all configurations to defaults") ||
		!strings.Contains(response.Body.String(), "network_ping_jitter_seconds") ||
		!strings.Contains(response.Body.String(), "<th>Jitter</th>") ||
		!strings.Contains(response.Body.String(), "Run Ookla now") ||
		!strings.Contains(response.Body.String(), "ookla-next-run") ||
		!strings.Contains(response.Body.String(), "cloudflare-next-run") ||
		!strings.Contains(response.Body.String(), "speedtest_ookla_cron") ||
		!strings.Contains(response.Body.String(), "speedtest_terms_accepted") ||
		!strings.Contains(response.Body.String(), "Cloudflare edge network") ||
		!strings.Contains(response.Body.String(), "speedtest_cloudflare_notice_accepted") ||
		!strings.Contains(response.Body.String(), "cloudflare-concurrent-requests") ||
		!strings.Contains(response.Body.String(), "cloudflare-request-size-mb") ||
		!strings.Contains(response.Body.String(), "Add target") ||
		!strings.Contains(response.Body.String(), "remove-target") ||
		strings.Contains(response.Body.String(), "prometheus_url") ||
		strings.Contains(response.Body.String(), "grafana_url") {
		t.Fatalf("unexpected page response: status=%d body=%q", response.Code, response.Body.String())
	}
	if pingSectionEnd < 0 || latestPingSection < pingSectionEnd || speedtestSection < latestPingSection {
		t.Fatalf("latest ping checks should appear after ping configuration and before speedtests")
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
