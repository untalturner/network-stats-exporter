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
	PacketsSent     uint64
	PacketsReceived uint64
}

type ProbeFunc func(context.Context, string, time.Duration, int) ProbeResult

type App struct {
	configPath string
	probe      ProbeFunc

	mu      sync.RWMutex
	config  Config
	results map[string]ProbeResult
}

func NewApp(configPath string, probe ProbeFunc) (*App, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &App{
		configPath: configPath,
		probe:      probe,
		config:     config,
		results:    make(map[string]ProbeResult),
	}, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/config", a.handleConfig)
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
		a.mu.Lock()
		defer a.mu.Unlock()
		if err := saveConfig(a.configPath, config); err != nil {
			http.Error(w, "save configuration failed", http.StatusInternalServerError)
			return
		}
		a.config = config
		writeJSON(w, http.StatusOK, config)
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	a.mu.RLock()
	defer a.mu.RUnlock()
	fmt.Fprintln(w, "# HELP network_ping_success Whether the most recent ICMP ping succeeded.")
	fmt.Fprintln(w, "# TYPE network_ping_success gauge")
	fmt.Fprintln(w, "# HELP network_ping_duration_seconds Round-trip latency of the most recent successful ping.")
	fmt.Fprintln(w, "# TYPE network_ping_duration_seconds gauge")
	fmt.Fprintln(w, "# HELP network_ping_packet_loss_ratio Fraction of ICMP echo requests lost in the most recent ping check; 1 means no reply.")
	fmt.Fprintln(w, "# TYPE network_ping_packet_loss_ratio gauge")
	for _, target := range a.config.Targets {
		labels := `target="` + escapeLabel(target) + `"`
		result, ok := a.results[target]
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
	for {
		a.mu.RLock()
		config := Config{
			Targets:             append([]string(nil), a.config.Targets...),
			ScheduleSeconds:     a.config.ScheduleSeconds,
			DelayMilliseconds:   a.config.DelayMilliseconds,
			TimeoutMilliseconds: a.config.TimeoutMilliseconds,
			PacketsPerPing:      a.config.PacketsPerPing,
		}
		a.mu.RUnlock()

		started := time.Now()
		for i, target := range config.Targets {
			result := a.probe(ctx, target, time.Duration(config.TimeoutMilliseconds)*time.Millisecond, config.PacketsPerPing)
			a.mu.Lock()
			a.results[target] = result
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
