package main

import (
	"context"
	"math"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var pingTimePattern = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)
var pingSequencePattern = regexp.MustCompile(`icmp_seq=(\d+)`)
var unixPingSummaryPattern = regexp.MustCompile(`(?m)(\d+)\s+packets transmitted,\s*(\d+)(?:\s+packets)?\s+received`)
var windowsPingSummaryPattern = regexp.MustCompile(`(?i)Packets:\s*Sent\s*=\s*(\d+),\s*Received\s*=\s*(\d+)`)

type pingRTT struct {
	sequence    uint64
	hasSequence bool
	duration    time.Duration
	segment     int
}

func pingHost(ctx context.Context, target string, timeout time.Duration, packetCount int) ProbeResult {
	started := time.Now()
	if timeout <= 0 {
		timeout = time.Second
	}
	if packetCount < 1 {
		packetCount = 1
	}
	// Allow for platform ping pacing plus the configured reply timeout.
	pingCtx, cancel := context.WithTimeout(ctx, timeout+time.Duration(packetCount+1)*time.Second)
	defer cancel()

	args := pingArgs(runtime.GOOS, target, timeout, packetCount)
	output, err := exec.CommandContext(pingCtx, "ping", args...).CombinedOutput()
	duration := time.Since(started)
	result := ProbeResult{Duration: duration}
	result.PacketsSent, result.PacketsReceived = parsePingPacketStatistics(output)
	if samples := parsePingRTTs(output); len(samples) > 0 {
		result.Duration = samples[0].duration
		result.Jitter, result.JitterAvailable = calculatePingJitter(samples)
	} else if match := pingTimePattern.FindSubmatch(output); len(match) == 2 {
		if milliseconds, parseErr := strconv.ParseFloat(string(match[1]), 64); parseErr == nil {
			duration = time.Duration(milliseconds * float64(time.Millisecond))
			result.Duration = duration
		}
	}
	result.Success = result.PacketsReceived > 0 || err == nil
	return result
}

func parsePingRTTs(output []byte) []pingRTT {
	var samples []pingRTT
	segment := 0
	for _, line := range strings.Split(string(output), "\n") {
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "request timed out") || strings.Contains(lowerLine, "request timeout") {
			segment++
			continue
		}
		match := pingTimePattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		milliseconds, err := strconv.ParseFloat(match[1], 64)
		if err != nil || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds < 0 ||
			milliseconds > float64(math.MaxInt64)/float64(time.Millisecond) {
			continue
		}
		sample := pingRTT{duration: time.Duration(milliseconds * float64(time.Millisecond)), segment: segment}
		if sequence := pingSequencePattern.FindStringSubmatch(line); len(sequence) == 2 {
			if value, parseErr := strconv.ParseUint(sequence[1], 10, 64); parseErr == nil {
				sample.sequence = value
				sample.hasSequence = true
			}
		}
		samples = append(samples, sample)
	}
	return samples
}

func calculatePingJitter(samples []pingRTT) (time.Duration, bool) {
	var total float64
	pairs := 0
	for i := 1; i < len(samples); i++ {
		previous, current := samples[i-1], samples[i]
		if previous.segment != current.segment ||
			(previous.hasSequence && current.hasSequence && current.sequence != previous.sequence+1) {
			continue
		}
		difference := current.duration - previous.duration
		if difference < 0 {
			difference = -difference
		}
		total += float64(difference)
		pairs++
	}
	if pairs == 0 {
		return 0, false
	}
	return time.Duration(total / float64(pairs)), true
}

func pingArgs(goos, target string, timeout time.Duration, packetCount int) []string {
	timeoutMilliseconds := int((timeout + time.Millisecond - 1) / time.Millisecond)
	switch goos {
	case "windows":
		return []string{"-n", strconv.Itoa(packetCount), "-w", strconv.Itoa(timeoutMilliseconds), target}
	case "darwin":
		return []string{"-n", "-c", strconv.Itoa(packetCount), "-i", "0.2", "-W",
			strconv.Itoa(timeoutMilliseconds), target}
	case "linux":
		return []string{"-n", "-c", strconv.Itoa(packetCount), "-i", "0.2", "-W",
			strconv.FormatFloat(timeout.Seconds(), 'f', 3, 64), target}
	default:
		return []string{"-n", "-c", strconv.Itoa(packetCount), "-W", strconv.Itoa(timeoutMilliseconds), target}
	}
}

func parsePingPacketStatistics(output []byte) (sent, received uint64) {
	match := unixPingSummaryPattern.FindSubmatch(output)
	if len(match) != 3 {
		match = windowsPingSummaryPattern.FindSubmatch(output)
	}
	if len(match) == 3 {
		sent, _ = strconv.ParseUint(string(match[1]), 10, 64)
		received, _ = strconv.ParseUint(string(match[2]), 10, 64)
	}
	return sent, received
}
