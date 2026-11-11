package main

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var pingTimePattern = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)

func pingHost(ctx context.Context, target string, timeout time.Duration) ProbeResult {
	started := time.Now()
	commandTimeout := timeout
	if commandTimeout <= 0 {
		commandTimeout = time.Second
	}
	pingCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	timeoutSeconds := int((timeout + time.Second - 1) / time.Second)
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	output, err := exec.CommandContext(pingCtx, "ping", "-n", "-c", "1", "-W",
		strconv.Itoa(timeoutSeconds), target).CombinedOutput()
	duration := time.Since(started)
	if err != nil {
		return ProbeResult{Duration: duration}
	}
	if match := pingTimePattern.FindSubmatch(output); len(match) == 2 {
		if milliseconds, parseErr := strconv.ParseFloat(string(match[1]), 64); parseErr == nil {
			duration = time.Duration(milliseconds * float64(time.Millisecond))
		}
	}
	return ProbeResult{Success: true, Duration: duration}
}
