package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	Provider              string     `json:"provider"`
	RunningProvider       string     `json:"running_provider,omitempty"`
	LastRunAt             *time.Time `json:"last_run_at,omitempty"`
	NextRunAt             *time.Time `json:"next_run_at,omitempty"`
	DownloadBitsPerSecond float64    `json:"download_bits_per_second,omitempty"`
	UploadBitsPerSecond   float64    `json:"upload_bits_per_second,omitempty"`
	LatencyMilliseconds   float64    `json:"latency_milliseconds,omitempty"`
	PacketLossPercent     *float64   `json:"packet_loss_percent,omitempty"`
	Error                 string     `json:"error,omitempty"`
}

type SpeedtestFunc func(context.Context) (SpeedtestMeasurement, error)
type CloudflareSpeedtestFunc func(context.Context, int, int) (SpeedtestMeasurement, error)

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

const (
	cloudflareSpeedtestURL       = "https://speed.cloudflare.com"
	cloudflareTransferBytesPerMB = int64(1_000_000)
	cloudflareLatencySampleCount = 5
)

var cloudflareRequestSequence atomic.Uint64

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func runCloudflareSpeedtest(ctx context.Context, concurrentRequests int, requestSizeMB int) (SpeedtestMeasurement, error) {
	return runCloudflareSpeedtestAt(ctx, http.DefaultClient, cloudflareSpeedtestURL, concurrentRequests, requestSizeMB)
}

func runCloudflareSpeedtestAt(ctx context.Context, client *http.Client, baseURL string, concurrentRequests int, requestSizeMB int) (SpeedtestMeasurement, error) {
	if concurrentRequests < 1 || concurrentRequests > 8 {
		return SpeedtestMeasurement{}, fmt.Errorf("Cloudflare concurrent requests must be between 1 and 8")
	}
	if requestSizeMB < 1 || requestSizeMB > 50 {
		return SpeedtestMeasurement{}, fmt.Errorf("Cloudflare request size must be between 1 and 50 MB")
	}
	latencies := make([]time.Duration, 0, cloudflareLatencySampleCount)
	for range cloudflareLatencySampleCount {
		started := time.Now()
		if err := cloudflareRequest(ctx, client, http.MethodGet, baseURL, "/__down", 0); err != nil {
			return SpeedtestMeasurement{}, fmt.Errorf("Cloudflare latency check: %w", err)
		}
		latencies = append(latencies, time.Since(started))
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	transferBytes := int64(requestSizeMB) * cloudflareTransferBytesPerMB
	downloadBitsPerSecond, err := runCloudflareTransfer(ctx, client, baseURL, http.MethodGet, concurrentRequests, transferBytes)
	if err != nil {
		return SpeedtestMeasurement{}, fmt.Errorf("Cloudflare download test: %w", err)
	}
	uploadBitsPerSecond, err := runCloudflareTransfer(ctx, client, baseURL, http.MethodPost, concurrentRequests, transferBytes)
	if err != nil {
		return SpeedtestMeasurement{}, fmt.Errorf("Cloudflare upload test: %w", err)
	}
	return SpeedtestMeasurement{
		DownloadBitsPerSecond: downloadBitsPerSecond,
		UploadBitsPerSecond:   uploadBitsPerSecond,
		Latency:               latencies[len(latencies)/2],
	}, nil
}

func runCloudflareTransfer(ctx context.Context, client *http.Client, baseURL string, method string, concurrentRequests int, transferBytes int64) (float64, error) {
	requestContext, cancel := context.WithCancel(ctx)
	defer cancel()

	type transferResult struct {
		bytes int64
		err   error
	}
	results := make(chan transferResult, concurrentRequests)
	var workers sync.WaitGroup
	started := time.Now()
	for range concurrentRequests {
		workers.Add(1)
		go func() {
			defer workers.Done()
			path := "/__down"
			if method == http.MethodPost {
				path = "/__up"
			}
			transferred, err := cloudflareRequestBytes(requestContext, client, method, baseURL, path, transferBytes)
			if err != nil {
				cancel()
			}
			results <- transferResult{bytes: transferred, err: err}
		}()
	}
	workers.Wait()
	close(results)

	var totalBytes int64
	var transferErr error
	for result := range results {
		if result.err != nil {
			if transferErr == nil || (errors.Is(transferErr, context.Canceled) && !errors.Is(result.err, context.Canceled)) {
				transferErr = result.err
			}
			continue
		}
		totalBytes += result.bytes
	}
	if transferErr != nil {
		return 0, transferErr
	}
	elapsed := time.Since(started)
	if elapsed <= 0 || totalBytes == 0 {
		return 0, fmt.Errorf("no transfer data was measured")
	}
	return float64(totalBytes) * 8 / elapsed.Seconds(), nil
}

func cloudflareRequestBytes(ctx context.Context, client *http.Client, method string, baseURL string, path string, byteCount int64) (int64, error) {
	var body io.Reader
	if method == http.MethodPost {
		body = io.LimitReader(zeroReader{}, byteCount)
	}
	endpoint, err := cloudflareMeasurementURL(baseURL, path, byteCount)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	if method == http.MethodPost {
		request.ContentLength = byteCount
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return 0, fmt.Errorf("HTTP %s", response.Status)
	}
	if method == http.MethodPost {
		if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 4096)); err != nil {
			return 0, fmt.Errorf("read upload response: %w", err)
		}
		return byteCount, nil
	}
	transferred, err := io.Copy(io.Discard, io.LimitReader(response.Body, byteCount+1))
	if err != nil {
		return 0, err
	}
	if transferred != byteCount {
		return 0, fmt.Errorf("expected %d response bytes, received %d", byteCount, transferred)
	}
	return transferred, nil
}

func cloudflareRequest(ctx context.Context, client *http.Client, method string, baseURL string, path string, byteCount int64) error {
	endpoint, err := cloudflareMeasurementURL(baseURL, path, byteCount)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 4096)); err != nil {
		return fmt.Errorf("read latency response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP %s", response.Status)
	}
	return nil
}

func cloudflareMeasurementURL(baseURL string, path string, byteCount int64) (string, error) {
	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return "", fmt.Errorf("invalid Cloudflare test URL %q", baseURL)
	}
	if endpoint.Host == "" {
		return "", fmt.Errorf("invalid Cloudflare test URL %q", baseURL)
	}
	query := endpoint.Query()
	query.Set("bytes", strconv.FormatInt(byteCount, 10))
	query.Set("cacheBust", strconv.FormatUint(cloudflareRequestSequence.Add(1), 10))
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}
