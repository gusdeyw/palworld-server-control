package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type palworldLogEvent struct {
	Timestamp  string `json:"timestamp"`
	Event      string `json:"event"`
	PlayerName string `json:"playername"`
	Details    []any  `json:"details"`
}

func filterPalworldLogLines(raw []string, limit int) []string {
	if limit < 1 {
		return []string{}
	}

	filtered := make([]string, 0, min(len(raw), limit))
	var record []string
	var recordDockerTime string

	flushRecord := func() {
		if len(record) == 0 {
			return
		}
		content := strings.Join(record, "\n")
		var event palworldLogEvent
		if json.Unmarshal([]byte(content), &event) == nil {
			if !isRESTPollingEvent(event) {
				filtered = append(filtered, formatPalworldEvent(event, recordDockerTime))
			}
		} else {
			for _, line := range record {
				line = strings.TrimSpace(line)
				if line != "" && !isRESTNoise(line) {
					filtered = append(filtered, formatConsoleLine(recordDockerTime, line))
				}
			}
		}
		record = nil
		recordDockerTime = ""
	}

	for _, rawLine := range raw {
		dockerTime, content := splitDockerTimestamp(rawLine)
		trimmed := strings.TrimSpace(content)
		if len(record) > 0 {
			record = append(record, content)
			if json.Valid([]byte(strings.Join(record, "\n"))) {
				flushRecord()
			}
			continue
		}
		if strings.HasPrefix(trimmed, "{") {
			record = []string{content}
			recordDockerTime = dockerTime
			if json.Valid([]byte(content)) {
				flushRecord()
			}
			continue
		}
		if trimmed == "" || isRESTNoise(trimmed) {
			continue
		}
		filtered = append(filtered, formatConsoleLine(dockerTime, trimmed))
	}
	flushRecord()

	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}

func splitDockerTimestamp(line string) (string, string) {
	first, rest, found := strings.Cut(strings.TrimRight(line, "\r"), " ")
	if !found {
		return "", line
	}
	if parsed, err := time.Parse(time.RFC3339Nano, first); err == nil {
		return parsed.UTC().Format("2006-01-02 15:04:05"), rest
	}
	return "", line
}

func isRESTPollingEvent(event palworldLogEvent) bool {
	if strings.EqualFold(strings.TrimSpace(event.PlayerName), "REST") {
		return true
	}
	for _, detail := range event.Details {
		if strings.HasPrefix(strings.TrimSpace(fmt.Sprint(detail)), "/v1/api/") {
			return true
		}
	}
	return false
}

func isRESTNoise(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "/v1/api/") ||
		(strings.Contains(lower, `"playername"`) && strings.Contains(lower, `"rest"`))
}

func formatPalworldEvent(event palworldLogEvent, dockerTime string) string {
	timestamp := strings.TrimSpace(event.Timestamp)
	if timestamp == "" {
		timestamp = dockerTime
	}
	parts := make([]string, 0, 3)
	if name := strings.TrimSpace(event.Event); name != "" {
		parts = append(parts, strings.ToUpper(name))
	} else {
		parts = append(parts, "PALWORLD")
	}
	if player := strings.TrimSpace(event.PlayerName); player != "" {
		parts = append(parts, player)
	}
	details := make([]string, 0, len(event.Details))
	for _, detail := range event.Details {
		value := strings.TrimSpace(fmt.Sprint(detail))
		if value != "" {
			details = append(details, value)
		}
	}
	message := strings.Join(parts, " · ")
	if len(details) > 0 {
		message += " — " + strings.Join(details, " · ")
	}
	return formatConsoleLine(timestamp, message)
}

func formatConsoleLine(timestamp, message string) string {
	message = strings.TrimSpace(message)
	if timestamp == "" {
		return message
	}
	return "[" + timestamp + "] " + message
}
