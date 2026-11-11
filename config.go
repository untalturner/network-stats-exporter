package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	Targets             []string `json:"targets"`
	ScheduleSeconds     int      `json:"schedule_seconds"`
	DelayMilliseconds   int      `json:"delay_milliseconds"`
	TimeoutMilliseconds int      `json:"timeout_milliseconds"`
}

func defaultConfig() Config {
	return Config{
		Targets:             []string{"1.1.1.1", "8.8.8.8"},
		ScheduleSeconds:     60,
		DelayMilliseconds:   1000,
		TimeoutMilliseconds: 2000,
	}
}

var hostnamePattern = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.?$`)

func validateConfig(config Config) (Config, error) {
	if len(config.Targets) == 0 || len(config.Targets) > 50 {
		return Config{}, fmt.Errorf("provide between 1 and 50 targets")
	}
	for i, target := range config.Targets {
		target = strings.TrimSpace(target)
		if target == "" || len(target) > 253 || strings.ContainsAny(target, "\r\n\t ") {
			return Config{}, fmt.Errorf("target %d is empty or invalid", i+1)
		}
		if net.ParseIP(target) == nil && !hostnamePattern.MatchString(target) {
			return Config{}, fmt.Errorf("target %q must be an IP address or hostname", target)
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
