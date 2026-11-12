package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ProbeResult struct {
	Success         bool
	Duration        time.Duration
	Jitter          time.Duration
	JitterAvailable bool
	PacketsSent     uint64
	PacketsReceived uint64
}

type ProbeFunc func(context.Context, string, time.Duration, int) ProbeResult

type speedtestStartResult int

const (
	speedtestStartSucceeded speedtestStartResult = iota
	speedtestStartAlreadyRunning
	speedtestStartAcknowledgementRequired
)

type App struct {
	configPath          string
	probe               ProbeFunc
	speedtest           SpeedtestFunc
	cloudflareSpeedtest SpeedtestFunc

	mu              sync.RWMutex
	config          Config
	results         map[string]ProbeResult
	speedtestStatus SpeedtestStatus
	runContext      context.Context
}

func NewApp(configPath string, probe ProbeFunc) (*App, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &App{
		configPath:          configPath,
		probe:               probe,
		speedtest:           runOoklaSpeedtest,
		cloudflareSpeedtest: runCloudflareSpeedtest,
		config:              config,
		results:             make(map[string]ProbeResult),
		runContext:          context.Background(),
	}, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/config/reset", a.handleConfigReset)
	mux.HandleFunc("/api/speedtest", a.handleSpeedtestStatus)
	mux.HandleFunc("/api/speedtest/run", a.handleSpeedtestRun)
	mux.HandleFunc("/metrics", a.handleMetrics)
	return mux
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.RLock()
		config := a.config
		a.mu.RUnlock()
		writeJSON(w, http.StatusOK, config)
	case http.MethodPut, http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var config Config
		if err := decoder.Decode(&config); err != nil {
			http.Error(w, "invalid configuration JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "request must contain one JSON object", http.StatusBadRequest)
			return
		}
		config, err := validateConfig(config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.saveConfiguration(w, config)
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.saveConfiguration(w, defaultConfig())
}

func (a *App) handleSpeedtestStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.mu.RLock()
	status := a.speedtestStatus
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, status)
}

func (a *App) handleSpeedtestRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result, provider := a.startSpeedtest(nil)
	if result == speedtestStartAcknowledgementRequired {
		http.Error(w, speedtestAcknowledgementMessage(provider), http.StatusForbidden)
		return
	}
	if result == speedtestStartAlreadyRunning {
		http.Error(w, "a speedtest is already running", http.StatusConflict)
		return
	}
	a.mu.RLock()
	status := a.speedtestStatus
	a.mu.RUnlock()
	writeJSON(w, http.StatusAccepted, status)
}

func (a *App) startSpeedtest(ctx context.Context) (speedtestStartResult, string) {
	a.mu.Lock()
	if a.speedtestStatus.Running {
		a.mu.Unlock()
		return speedtestStartAlreadyRunning, ""
	}
	if !a.config.speedtestAcknowledged() {
		provider := a.config.SpeedtestProvider
		a.mu.Unlock()
		return speedtestStartAcknowledgementRequired, provider
	}
	if ctx == nil {
		ctx = a.runContext
	}
	timeout := time.Duration(a.config.SpeedtestTimeoutSeconds) * time.Second
	provider := a.config.SpeedtestProvider
	speedtest := a.speedtest
	if provider == speedtestProviderCloudflare {
		speedtest = a.cloudflareSpeedtest
	}
	a.speedtestStatus.Running = true
	a.speedtestStatus.RunningProvider = provider
	a.mu.Unlock()

	go func() {
		testContext, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		measurement, err := speedtest(testContext)
		completedAt := time.Now().UTC()
		a.mu.Lock()
		defer a.mu.Unlock()
		a.speedtestStatus.Running = false
		a.speedtestStatus.RunningProvider = ""
		a.speedtestStatus.Attempted = true
		a.speedtestStatus.Success = err == nil
		a.speedtestStatus.Provider = provider
		a.speedtestStatus.LastRunAt = &completedAt
		a.speedtestStatus.Error = ""
		a.speedtestStatus.DownloadBitsPerSecond = 0
		a.speedtestStatus.UploadBitsPerSecond = 0
		a.speedtestStatus.LatencyMilliseconds = 0
		a.speedtestStatus.PacketLossPercent = nil
		if err != nil {
			a.speedtestStatus.Error = err.Error()
			return
		}
		a.speedtestStatus.DownloadBitsPerSecond = measurement.DownloadBitsPerSecond
		a.speedtestStatus.UploadBitsPerSecond = measurement.UploadBitsPerSecond
		a.speedtestStatus.LatencyMilliseconds = float64(measurement.Latency) / float64(time.Millisecond)
		a.speedtestStatus.PacketLossPercent = measurement.PacketLossPercent
	}()
	return speedtestStartSucceeded, provider
}

func (a *App) runScheduledSpeedtests(ctx context.Context) {
	for {
		a.mu.RLock()
		enabled := a.config.SpeedtestEnabled && a.config.speedtestAcknowledged()
		interval := a.config.SpeedtestIntervalSeconds
		a.mu.RUnlock()
		waitFor := time.Second
		if enabled {
			waitFor = time.Duration(interval) * time.Second
		}
		if !wait(ctx, waitFor) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		a.mu.RLock()
		enabled = a.config.SpeedtestEnabled && a.config.speedtestAcknowledged()
		a.mu.RUnlock()
		if enabled {
			a.startSpeedtest(ctx)
		}
	}
}

func (a *App) saveConfiguration(w http.ResponseWriter, config Config) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := saveConfig(a.configPath, config); err != nil {
		http.Error(w, fmt.Sprintf("save configuration to %q failed: %v; ensure the mounted /data directory is writable by the exporter", a.configPath, err), http.StatusInternalServerError)
		return
	}
	a.config = config
	writeJSON(w, http.StatusOK, config)
}

func (a *App) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	a.mu.RLock()
	defer a.mu.RUnlock()
	provider := a.speedtestStatus.Provider
	if provider == "" {
		provider = a.speedtestStatus.RunningProvider
	}
	if provider == "" {
		provider = a.config.SpeedtestProvider
	}
	fmt.Fprintln(w, "# HELP network_ping_success Whether the most recent ICMP ping succeeded.")
	fmt.Fprintln(w, "# TYPE network_ping_success gauge")
	fmt.Fprintln(w, "# HELP network_ping_duration_seconds Round-trip latency of the most recent successful ping.")
	fmt.Fprintln(w, "# TYPE network_ping_duration_seconds gauge")
	fmt.Fprintln(w, "# HELP network_ping_jitter_seconds Mean absolute difference between consecutive packet RTTs in the most recent ping check.")
	fmt.Fprintln(w, "# TYPE network_ping_jitter_seconds gauge")
	fmt.Fprintln(w, "# HELP network_ping_packet_loss_ratio Fraction of ICMP echo requests lost in the most recent ping check; 1 means no reply.")
	fmt.Fprintln(w, "# TYPE network_ping_packet_loss_ratio gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_running Whether a speedtest is currently running.")
	fmt.Fprintln(w, "# TYPE network_speedtest_running gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_success Whether the most recent speedtest succeeded.")
	fmt.Fprintln(w, "# TYPE network_speedtest_success gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_download_bits_per_second Download throughput from the most recent successful speedtest.")
	fmt.Fprintln(w, "# TYPE network_speedtest_download_bits_per_second gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_upload_bits_per_second Upload throughput from the most recent successful speedtest.")
	fmt.Fprintln(w, "# TYPE network_speedtest_upload_bits_per_second gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_latency_seconds Latency from the most recent successful speedtest.")
	fmt.Fprintln(w, "# TYPE network_speedtest_latency_seconds gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_packet_loss_ratio Packet loss fraction from the most recent successful speedtest.")
	fmt.Fprintln(w, "# TYPE network_speedtest_packet_loss_ratio gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_last_run_timestamp_seconds Unix timestamp of the most recent completed speedtest.")
	fmt.Fprintln(w, "# TYPE network_speedtest_last_run_timestamp_seconds gauge")
	fmt.Fprintln(w, "# HELP network_speedtest_provider_info Provider used for the most recent speedtest, or the configured provider before the first run.")
	fmt.Fprintln(w, "# TYPE network_speedtest_provider_info gauge")
	fmt.Fprintf(w, "network_speedtest_provider_info{provider=%q} 1\n", escapeLabel(provider))
	if a.speedtestStatus.Running {
		fmt.Fprintln(w, "network_speedtest_running 1")
	} else {
		fmt.Fprintln(w, "network_speedtest_running 0")
	}
	if a.speedtestStatus.Attempted {
		success := 0
		if a.speedtestStatus.Success {
			success = 1
			fmt.Fprintf(w, "network_speedtest_download_bits_per_second %g\n", a.speedtestStatus.DownloadBitsPerSecond)
			fmt.Fprintf(w, "network_speedtest_upload_bits_per_second %g\n", a.speedtestStatus.UploadBitsPerSecond)
			fmt.Fprintf(w, "network_speedtest_latency_seconds %g\n", a.speedtestStatus.LatencyMilliseconds/1000)
			if a.speedtestStatus.PacketLossPercent != nil {
				fmt.Fprintf(w, "network_speedtest_packet_loss_ratio %g\n", *a.speedtestStatus.PacketLossPercent/100)
			}
		}
		fmt.Fprintf(w, "network_speedtest_success %d\n", success)
		if a.speedtestStatus.LastRunAt != nil {
			fmt.Fprintf(w, "network_speedtest_last_run_timestamp_seconds %g\n", float64(a.speedtestStatus.LastRunAt.UnixNano())/float64(time.Second))
		}
	}
	for _, target := range a.config.Targets {
		labels := `target="` + escapeLabel(target.Address) + `",type="` + escapeLabel(target.Type) + `"`
		result, ok := a.results[target.Address]
		if !ok {
			continue
		}
		success := 0
		if result.Success {
			success = 1
		}
		fmt.Fprintf(w, "network_ping_success{%s} %d\n", labels, success)
		if result.Success {
			fmt.Fprintf(w, "network_ping_duration_seconds{%s} %g\n", labels, result.Duration.Seconds())
		}
		if result.JitterAvailable {
			fmt.Fprintf(w, "network_ping_jitter_seconds{%s} %g\n", labels, result.Jitter.Seconds())
		}
		lossRatio := 1.0
		if result.PacketsSent > 0 {
			packetsLost := uint64(0)
			if result.PacketsSent > result.PacketsReceived {
				packetsLost = result.PacketsSent - result.PacketsReceived
			}
			lossRatio = float64(packetsLost) / float64(result.PacketsSent)
		} else if result.Success {
			lossRatio = 0
		}
		fmt.Fprintf(w, "network_ping_packet_loss_ratio{%s} %g\n", labels, lossRatio)
	}
}

func (a *App) run(ctx context.Context) {
	a.mu.Lock()
	a.runContext = ctx
	a.mu.Unlock()
	go a.runScheduledSpeedtests(ctx)
	for {
		a.mu.RLock()
		config := Config{
			Targets:             append([]Target(nil), a.config.Targets...),
			ScheduleSeconds:     a.config.ScheduleSeconds,
			DelayMilliseconds:   a.config.DelayMilliseconds,
			TimeoutMilliseconds: a.config.TimeoutMilliseconds,
			PacketsPerPing:      a.config.PacketsPerPing,
		}
		a.mu.RUnlock()

		started := time.Now()
		for i, target := range config.Targets {
			result := a.probe(ctx, target.Address, time.Duration(config.TimeoutMilliseconds)*time.Millisecond, config.PacketsPerPing)
			a.mu.Lock()
			a.results[target.Address] = result
			a.mu.Unlock()

			if i < len(config.Targets)-1 && config.DelayMilliseconds > 0 {
				if !wait(ctx, time.Duration(config.DelayMilliseconds)*time.Millisecond) {
					return
				}
			}
		}
		nextRun := time.Duration(config.ScheduleSeconds)*time.Second - time.Since(started)
		if nextRun > 0 && !wait(ctx, nextRun) {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func escapeLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return replacer.Replace(value)
}

func speedtestAcknowledgementMessage(provider string) string {
	if provider == speedtestProviderCloudflare {
		return "acknowledge the Cloudflare privacy notice in the configuration before running a speedtest"
	}
	return "accept the Ookla Speedtest CLI terms in the configuration before running a speedtest"
}
