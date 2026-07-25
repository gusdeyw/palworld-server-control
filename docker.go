package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type DockerStats struct {
	CPUPercent    string `json:"cpuPercent"`
	MemoryUsage   string `json:"memoryUsage"`
	MemoryPercent string `json:"memoryPercent"`
	NetworkIO     string `json:"networkIO"`
	BlockIO       string `json:"blockIO"`
}

type dockerStatsJSON struct {
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
}

type DockerManager struct {
	container  string
	composeDir string
	service    string
	controlURL string
	controlKey string
	mock       bool
}

type nativeControlResponse struct {
	Status  string      `json:"status"`
	Stats   DockerStats `json:"stats"`
	Message string      `json:"message"`
	Lines   []string    `json:"lines"`
	Error   string      `json:"error"`
}

func NewDockerManager(
	container, composeDir, service, controlURL, controlKey string,
	mock bool,
) *DockerManager {
	return &DockerManager{
		container:  strings.TrimSpace(container),
		composeDir: strings.TrimSpace(composeDir),
		service:    strings.TrimSpace(service),
		controlURL: strings.TrimRight(strings.TrimSpace(controlURL), "/"),
		controlKey: strings.TrimSpace(controlKey),
		mock:       mock,
	}
}

func (d *DockerManager) Status(ctx context.Context) (string, error) {
	if d.mock {
		return "running", nil
	}
	if d.controlURL != "" {
		response, err := d.nativeRequest(ctx, http.MethodGet, "/status")
		if err != nil {
			return "unavailable", err
		}
		return response.Status, nil
	}
	output, err := d.run(ctx, "inspect", "--format", "{{.State.Status}}", d.container)
	if err != nil {
		return "unavailable", err
	}
	return strings.TrimSpace(output), nil
}

func (d *DockerManager) Stats(ctx context.Context) (DockerStats, error) {
	if d.mock {
		phase := time.Now().Unix() % 20
		return DockerStats{
			CPUPercent:    strconv.FormatInt(18+phase/4, 10) + ".4%",
			MemoryUsage:   "7.82GiB / 16GiB",
			MemoryPercent: "48.9%",
			NetworkIO:     "1.4GB / 963MB",
			BlockIO:       "4.8GB / 2.1GB",
		}, nil
	}
	if d.controlURL != "" {
		response, err := d.nativeRequest(ctx, http.MethodGet, "/status")
		if err != nil {
			return DockerStats{}, err
		}
		return response.Stats, nil
	}
	output, err := d.run(ctx, "stats", "--no-stream", "--format", "{{json .}}", d.container)
	if err != nil {
		return DockerStats{}, err
	}
	var raw dockerStatsJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &raw); err != nil {
		return DockerStats{}, fmt.Errorf("decode docker stats: %w", err)
	}
	return DockerStats{
		CPUPercent:    raw.CPUPerc,
		MemoryUsage:   raw.MemUsage,
		MemoryPercent: raw.MemPerc,
		NetworkIO:     raw.NetIO,
		BlockIO:       raw.BlockIO,
	}, nil
}

func (d *DockerManager) Start(ctx context.Context) (string, error) {
	if d.mock {
		return "Server started", nil
	}
	if d.controlURL != "" {
		return d.nativeAction(ctx, "/start")
	}
	output, err := d.run(ctx, "start", d.container)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return "Server started", nil
	}
	return strings.TrimSpace(output), nil
}

func (d *DockerManager) Stop(ctx context.Context) (string, error) {
	if d.mock {
		return "Server stopped", nil
	}
	if d.controlURL != "" {
		return d.nativeAction(ctx, "/stop")
	}
	output, err := d.run(ctx, "stop", "--time", "30", d.container)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return "Server stopped", nil
	}
	return strings.TrimSpace(output), nil
}

func (d *DockerManager) Restart(ctx context.Context) (string, error) {
	if d.mock {
		return "Server restarted", nil
	}
	if d.controlURL != "" {
		return d.nativeAction(ctx, "/restart")
	}
	output, err := d.run(ctx, "restart", "--time", "30", d.container)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return "Server restarted", nil
	}
	return strings.TrimSpace(output), nil
}

func (d *DockerManager) Update(ctx context.Context) (string, error) {
	if d.mock {
		return "Sample update completed", nil
	}
	if d.controlURL != "" {
		return d.nativeAction(ctx, "/update")
	}
	if d.composeDir == "" {
		return "", errors.New("PALWORLD_COMPOSE_DIR is not configured")
	}
	if d.service == "" {
		return "", errors.New("PALWORLD_COMPOSE_SERVICE is not configured")
	}
	if _, err := d.runIn(ctx, d.composeDir, "compose", "pull", d.service); err != nil {
		return "", fmt.Errorf("pull image: %w", err)
	}
	if _, err := d.runIn(ctx, d.composeDir, "compose", "up", "-d", d.service); err != nil {
		return "", fmt.Errorf("recreate service: %w", err)
	}
	return "Server image updated", nil
}

func (d *DockerManager) Logs(ctx context.Context, tail int) ([]string, error) {
	if d.mock {
		now := time.Now()
		return []string{
			now.Add(-18*time.Second).Format(time.RFC3339) + " [Info] World autosave completed",
			now.Add(-12*time.Second).Format(time.RFC3339) + " [Info] Wina joined the server",
			now.Add(-8*time.Second).Format(time.RFC3339) + " [Info] REST API metrics request completed",
			now.Add(-3*time.Second).Format(time.RFC3339) + " [Info] Tick rate stable at 59.4 FPS",
		}, nil
	}
	if d.controlURL != "" {
		if tail < 1 || tail > 1000 {
			tail = 240
		}
		response, err := d.nativeRequest(
			ctx,
			http.MethodGet,
			"/logs?tail="+strconv.Itoa(tail),
		)
		if err != nil {
			return nil, err
		}
		return response.Lines, nil
	}
	if tail < 1 || tail > 1000 {
		tail = 240
	}
	output, err := d.run(ctx, "logs", "--tail", strconv.Itoa(tail), "--timestamps", d.container)
	if err != nil {
		return nil, err
	}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return []string{}, nil
	}
	return strings.Split(output, "\n"), nil
}

func (d *DockerManager) nativeAction(ctx context.Context, path string) (string, error) {
	response, err := d.nativeRequest(ctx, http.MethodPost, path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Message) == "" {
		return "Action completed", nil
	}
	return strings.TrimSpace(response.Message), nil
}

func (d *DockerManager) nativeRequest(
	ctx context.Context,
	method, path string,
) (nativeControlResponse, error) {
	var response nativeControlResponse
	request, err := http.NewRequestWithContext(ctx, method, d.controlURL+path, nil)
	if err != nil {
		return response, fmt.Errorf("create native control request: %w", err)
	}
	request.Header.Set("X-Pal-Control-Token", d.controlKey)
	httpResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		return response, fmt.Errorf("native control: %w", err)
	}
	defer httpResponse.Body.Close()
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		return response, fmt.Errorf("decode native control response: %w", err)
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		message := strings.TrimSpace(response.Error)
		if message == "" {
			message = httpResponse.Status
		}
		return response, fmt.Errorf("native control: %s", message)
	}
	return response, nil
}

func (d *DockerManager) run(ctx context.Context, args ...string) (string, error) {
	return d.runIn(ctx, "", args...)
}

func (d *DockerManager) runIn(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	if dir != "" {
		command.Dir = dir
	}
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("docker %s: %s", strings.Join(args, " "), message)
	}
	return string(output), nil
}
