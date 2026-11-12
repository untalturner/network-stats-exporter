package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Target struct {
	Address string `json:"address"`
	Type    string `json:"type"`
}

func (t *Target) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var address string
		if err := json.Unmarshal(data, &address); err != nil {
			return err
		}
		*t = Target{Address: address, Type: "default"}
		return nil
	}

	type targetFields Target
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var fields targetFields
	if err := decoder.Decode(&fields); err != nil {
		return err
	}
	*t = Target(fields)
	return nil
}

type Config struct {
	Targets                           []Target `json:"targets"`
	ScheduleSeconds                   int      `json:"schedule_seconds"`
	DelayMilliseconds                 int      `json:"delay_milliseconds"`
	TimeoutMilliseconds               int      `json:"timeout_milliseconds"`
	PacketsPerPing                    int      `json:"packets_per_ping"`
	SpeedtestEnabled                  bool     `json:"speedtest_enabled"`
	SpeedtestProvider                 string   `json:"speedtest_provider"`
	SpeedtestTermsAccepted            bool     `json:"speedtest_terms_accepted"`
	SpeedtestCloudflareNoticeAccepted bool     `json:"speedtest_cloudflare_notice_accepted"`
	SpeedtestIntervalSeconds          int      `json:"speedtest_interval_seconds"`
	SpeedtestTimeoutSeconds           int      `json:"speedtest_timeout_seconds"`
}

const (
	speedtestProviderOokla      = "ookla"
	speedtestProviderCloudflare = "cloudflare"
)

func defaultConfig() Config {
	return Config{
		Targets:                           []Target{{Address: "1.1.1.1", Type: "external"}, {Address: "8.8.8.8", Type: "external"}},
		ScheduleSeconds:                   1,
		DelayMilliseconds:                 0,
		TimeoutMilliseconds:               1000,
		PacketsPerPing:                    5,
		SpeedtestEnabled:                  false,
		SpeedtestProvider:                 speedtestProviderOokla,
		SpeedtestTermsAccepted:            false,
		SpeedtestCloudflareNoticeAccepted: false,
		SpeedtestIntervalSeconds:          1800,
		SpeedtestTimeoutSeconds:           300,
	}
}

func (config Config) speedtestAcknowledged() bool {
	if config.SpeedtestProvider == speedtestProviderCloudflare {
		return config.SpeedtestCloudflareNoticeAccepted
	}
	return config.SpeedtestTermsAccepted
}

var hostnamePattern = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.?$`)
var targetTypePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)

func validateConfig(config Config) (Config, error) {
	if config.PacketsPerPing == 0 {
		config.PacketsPerPing = defaultConfig().PacketsPerPing
	}
	if config.SpeedtestIntervalSeconds == 0 {
		config.SpeedtestIntervalSeconds = defaultConfig().SpeedtestIntervalSeconds
	}
	if config.SpeedtestTimeoutSeconds == 0 {
		config.SpeedtestTimeoutSeconds = defaultConfig().SpeedtestTimeoutSeconds
	}
	if config.SpeedtestProvider == "" {
		config.SpeedtestProvider = defaultConfig().SpeedtestProvider
	}
	if config.SpeedtestProvider != speedtestProviderOokla && config.SpeedtestProvider != speedtestProviderCloudflare {
		return Config{}, fmt.Errorf("speedtest provider must be %q or %q", speedtestProviderOokla, speedtestProviderCloudflare)
	}
	if len(config.Targets) == 0 || len(config.Targets) > 50 {
		return Config{}, fmt.Errorf("provide between 1 and 50 targets")
	}
	for i, target := range config.Targets {
		target.Address = strings.TrimSpace(target.Address)
		target.Type = strings.TrimSpace(target.Type)
		if target.Address == "" || len(target.Address) > 253 || strings.ContainsAny(target.Address, "\r\n\t ") {
			return Config{}, fmt.Errorf("target %d is empty or invalid", i+1)
		}
		if net.ParseIP(target.Address) == nil && !hostnamePattern.MatchString(target.Address) {
			return Config{}, fmt.Errorf("target %q must be an IP address or hostname", target.Address)
		}
		if !targetTypePattern.MatchString(target.Type) {
			return Config{}, fmt.Errorf("target %d type must start with a letter and contain only letters, numbers, underscores, or hyphens (up to 32 characters)", i+1)
		}
		config.Targets[i] = target
	}
	if config.ScheduleSeconds < 1 || config.ScheduleSeconds > 86400 {
		return Config{}, fmt.Errorf("schedule must be between 1 and 86400 seconds")
	}
	if config.DelayMilliseconds < 0 || config.DelayMilliseconds > 60000 {
		return Config{}, fmt.Errorf("delay must be between 0 and 60000 milliseconds")
	}
	if config.TimeoutMilliseconds < 100 || config.TimeoutMilliseconds > 60000 {
		return Config{}, fmt.Errorf("timeout must be between 100 and 60000 milliseconds")
	}
	if config.PacketsPerPing < 1 || config.PacketsPerPing > 20 {
		return Config{}, fmt.Errorf("packets per ping must be between 1 and 20")
	}
	if config.SpeedtestIntervalSeconds < 300 || config.SpeedtestIntervalSeconds > 604800 {
		return Config{}, fmt.Errorf("speedtest interval must be between 300 and 604800 seconds")
	}
	if config.SpeedtestTimeoutSeconds < 30 || config.SpeedtestTimeoutSeconds > 1800 {
		return Config{}, fmt.Errorf("speedtest timeout must be between 30 and 1800 seconds")
	}
	if config.SpeedtestEnabled && !config.speedtestAcknowledged() {
		if config.SpeedtestProvider == speedtestProviderCloudflare {
			return Config{}, fmt.Errorf("acknowledge the Cloudflare privacy notice before enabling automatic speedtests")
		}
		return Config{}, fmt.Errorf("accept the Ookla Speedtest CLI terms before enabling automatic speedtests")
	}
	return config, nil
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Config{}, fmt.Errorf("decode config fields: %w", err)
	}
	if _, exists := fields["packets_per_ping"]; !exists {
		config.PacketsPerPing = defaultConfig().PacketsPerPing
	}
	if _, exists := fields["speedtest_interval_seconds"]; !exists {
		config.SpeedtestIntervalSeconds = defaultConfig().SpeedtestIntervalSeconds
	}
	if _, exists := fields["speedtest_timeout_seconds"]; !exists {
		config.SpeedtestTimeoutSeconds = defaultConfig().SpeedtestTimeoutSeconds
	}
	return validateConfig(config)
}

func saveConfig(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
