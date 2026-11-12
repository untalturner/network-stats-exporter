package main

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

var pingTimePattern = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)
var unixPingSummaryPattern = regexp.MustCompile(`(?m)(\d+)\s+packets transmitted,\s*(\d+)(?:\s+packets)?\s+received`)
var windowsPingSummaryPattern = regexp.MustCompile(`(?i)Packets:\s*Sent\s*=\s*(\d+),\s*Received\s*=\s*(\d+)`)

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
	if match := pingTimePattern.FindSubmatch(output); len(match) == 2 {
		if milliseconds, parseErr := strconv.ParseFloat(string(match[1]), 64); parseErr == nil {
			duration = time.Duration(milliseconds * float64(time.Millisecond))
			result.Duration = duration
		}
	}
	result.Success = result.PacketsReceived > 0 || err == nil
	return result
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
