package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"time"
)

type SpeedtestMeasurement struct {
	DownloadBitsPerSecond float64       `json:"download_bits_per_second"`
	UploadBitsPerSecond   float64       `json:"upload_bits_per_second"`
	Latency               time.Duration `json:"-"`
	PacketLossPercent     *float64      `json:"packet_loss_percent,omitempty"`
}

type SpeedtestStatus struct {
	Running               bool       `json:"running"`
	Attempted             bool       `json:"attempted"`
	Success               bool       `json:"success"`
	LastRunAt             *time.Time `json:"last_run_at,omitempty"`
	DownloadBitsPerSecond float64    `json:"download_bits_per_second,omitempty"`
	UploadBitsPerSecond   float64    `json:"upload_bits_per_second,omitempty"`
	LatencyMilliseconds   float64    `json:"latency_milliseconds,omitempty"`
	PacketLossPercent     *float64   `json:"packet_loss_percent,omitempty"`
	Error                 string     `json:"error,omitempty"`
}

type SpeedtestFunc func(context.Context) (SpeedtestMeasurement, error)

func runOoklaSpeedtest(ctx context.Context) (SpeedtestMeasurement, error) {
	command := exec.CommandContext(ctx, "speedtest", "--format=json", "--progress=no", "--accept-license", "--accept-gdpr")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return SpeedtestMeasurement{}, fmt.Errorf("Ookla Speedtest CLI: %w: %s", err, stderr.String())
		}
		return SpeedtestMeasurement{}, fmt.Errorf("Ookla Speedtest CLI: %w", err)
	}
	measurement, err := parseOoklaSpeedtestJSON(output)
	if err != nil {
		return SpeedtestMeasurement{}, fmt.Errorf("parse Ookla Speedtest result: %w", err)
	}
	return measurement, nil
}

func parseOoklaSpeedtestJSON(data []byte) (SpeedtestMeasurement, error) {
	var result struct {
		Download struct {
			Bandwidth *float64 `json:"bandwidth"`
		} `json:"download"`
		Upload struct {
			Bandwidth *float64 `json:"bandwidth"`
		} `json:"upload"`
		Ping struct {
			Latency *float64 `json:"latency"`
		} `json:"ping"`
		PacketLoss *float64 `json:"packetLoss"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return SpeedtestMeasurement{}, err
	}
	if result.Download.Bandwidth == nil || result.Upload.Bandwidth == nil || result.Ping.Latency == nil {
		return SpeedtestMeasurement{}, fmt.Errorf("result is missing download bandwidth, upload bandwidth, or ping latency")
	}
	if !validNonnegativeFinite(*result.Download.Bandwidth) || !validNonnegativeFinite(*result.Upload.Bandwidth) ||
		!validNonnegativeFinite(*result.Ping.Latency) || *result.Download.Bandwidth > math.MaxFloat64/8 ||
		*result.Upload.Bandwidth > math.MaxFloat64/8 || *result.Ping.Latency > float64(math.MaxInt64)/float64(time.Millisecond) {
		return SpeedtestMeasurement{}, fmt.Errorf("result contains an invalid measurement")
	}
	measurement := SpeedtestMeasurement{
		DownloadBitsPerSecond: *result.Download.Bandwidth * 8,
		UploadBitsPerSecond:   *result.Upload.Bandwidth * 8,
		Latency:               time.Duration(*result.Ping.Latency * float64(time.Millisecond)),
	}
	if result.PacketLoss != nil {
		if !validNonnegativeFinite(*result.PacketLoss) || *result.PacketLoss > 100 {
			return SpeedtestMeasurement{}, fmt.Errorf("result contains an invalid packet loss percentage")
		}
		loss := *result.PacketLoss
		measurement.PacketLossPercent = &loss
	}
	return measurement, nil
}

func validNonnegativeFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
