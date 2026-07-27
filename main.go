package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

type Config struct {
	Addr             string
	PanelPassword    string
	SecureCookies    bool
	Mock             bool
	RestURL          string
	AdminUser        string
	AdminPassword    string
	RCONAddr         string
	RCONPassword     string
	Container        string
	ComposeDir       string
	ComposeService   string
	ControlURL       string
	ControlToken     string
	SaveDir          string
	BackupDir        string
	SettingsPath     string
	SettingsStateDir string
	MetricsInterval  time.Duration
	NetworkTargets   []string
	NetworkInterval  time.Duration
	NetworkTimeout   time.Duration
	NetworkWindow    int
	NetworkDegraded  float64
	NetworkCritical  float64
	ReportDir        string
	ReportRetention  int
	ReportTimezone   string
	ShutdownWait     int
	ShutdownMessage  string
}

type Sample struct {
	At          time.Time `json:"at"`
	FPS         float64   `json:"fps"`
	FrameTime   float64   `json:"frameTime"`
	Players     int       `json:"players"`
	MemoryUsage string    `json:"memoryUsage,omitempty"`
	LatencyMS   float64   `json:"latencyMs"`
	PacketLoss  float64   `json:"packetLoss"`
}

type App struct {
	cfg           Config
	pal           *PalClient
	docker        *DockerManager
	network       *NetworkMonitor
	reports       *ReportStore
	session       string
	historyMu     sync.RWMutex
	history       []Sample
	reportErrMu   sync.Mutex
	reportLastErr time.Time
	settingsMu    sync.Mutex
	mockSettings  map[string]any
}

type actionRequest struct {
	Action   string `json:"action"`
	Message  string `json:"message"`
	UserID   string `json:"userId"`
	WaitTime int    `json:"waitTime"`
}

type consoleRequest struct {
	Command string `json:"command"`
}

type loginRequest struct {
	Password string `json:"password"`
}

func main() {
	cfg := loadConfig()
	if err := validateConfig(cfg); err != nil {
		log.Fatal(err)
	}
	session, err := randomToken(32)
	if err != nil {
		log.Fatal(err)
	}
	reports, err := NewReportStore(cfg.ReportDir, cfg.ReportRetention, cfg.ReportTimezone)
	if err != nil {
		log.Fatal(err)
	}

	app := &App{
		cfg: cfg,
		pal: NewPalClient(cfg.RestURL, cfg.AdminUser, cfg.AdminPassword, cfg.Mock),
		docker: NewDockerManager(
			cfg.Container,
			cfg.ComposeDir,
			cfg.ComposeService,
			cfg.ControlURL,
			cfg.ControlToken,
			cfg.Mock,
		),
		network: NewNetworkMonitor(
			cfg.NetworkTargets,
			cfg.NetworkInterval,
			cfg.NetworkTimeout,
			cfg.NetworkWindow,
			cfg.NetworkDegraded,
			cfg.NetworkCritical,
			cfg.Mock,
		),
		reports: reports,
		session: session,
		history: make([]Sample, 0, 2880),
	}

	go app.sampleLoop()
	go app.network.Run(context.Background())

	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", app.handleLogin)
	mux.Handle("/api/logout", app.requireAuth(app.requireMutation(http.HandlerFunc(app.handleLogout))))
	mux.Handle("/api/state", app.requireAuth(http.HandlerFunc(app.handleState)))
	mux.Handle("/api/history", app.requireAuth(http.HandlerFunc(app.handleHistory)))
	mux.Handle("/api/reports", app.requireAuth(http.HandlerFunc(app.handleReports)))
	mux.Handle("/api/reports/", app.requireAuth(http.HandlerFunc(app.handleReports)))
	mux.Handle("/api/logs", app.requireAuth(http.HandlerFunc(app.handleLogs)))
	mux.Handle("/api/action", app.requireAuth(app.requireMutation(http.HandlerFunc(app.handleAction))))
	mux.Handle("/api/console", app.requireAuth(app.requireMutation(http.HandlerFunc(app.handleConsole))))
	mux.Handle("/api/backups", app.requireAuth(http.HandlerFunc(app.handleBackups)))
	mux.Handle("/api/backups/", app.requireAuth(app.requireMutation(http.HandlerFunc(app.handleDeleteBackup))))
	mux.Handle("/api/game-settings", app.requireAuth(http.HandlerFunc(app.handleGameSettings)))
	mux.Handle("/api/game-settings/apply", app.requireAuth(app.requireMutation(http.HandlerFunc(app.handleApplyGameSettings))))
	mux.Handle("/api/game-settings/rollback", app.requireAuth(app.requireMutation(http.HandlerFunc(app.handleRollbackGameSettings))))
	mux.Handle("/", http.FileServer(http.FS(staticRoot)))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("PAL CTRL listening on http://%s", cfg.Addr)
	log.Printf("Palworld REST endpoint: %s", cfg.RestURL)
	if cfg.Mock {
		log.Printf("Sample mode is enabled. No real server commands will run.")
	}
	log.Fatal(server.ListenAndServe())
}

func loadConfig() Config {
	adminPassword := env("PALWORLD_ADMIN_PASSWORD", "")
	backupDir := env("PALWORLD_BACKUP_DIR", "./backups")
	return Config{
		Addr:             env("PANEL_ADDR", "127.0.0.1:8080"),
		PanelPassword:    env("PANEL_PASSWORD", ""),
		SecureCookies:    envBool("PANEL_SECURE_COOKIES", false),
		Mock:             envBool("PALWORLD_MOCK", false),
		RestURL:          strings.TrimRight(env("PALWORLD_REST_URL", "http://127.0.0.1:8212"), "/"),
		AdminUser:        env("PALWORLD_ADMIN_USER", "admin"),
		AdminPassword:    adminPassword,
		RCONAddr:         env("PALWORLD_RCON_ADDR", "127.0.0.1:25575"),
		RCONPassword:     env("PALWORLD_RCON_PASSWORD", adminPassword),
		Container:        env("PALWORLD_CONTAINER", "palworld"),
		ComposeDir:       env("PALWORLD_COMPOSE_DIR", ""),
		ComposeService:   env("PALWORLD_COMPOSE_SERVICE", "palworld"),
		ControlURL:       env("PALWORLD_CONTROL_URL", ""),
		ControlToken:     env("PALWORLD_CONTROL_TOKEN", ""),
		SaveDir:          env("PALWORLD_SAVE_DIR", ""),
		BackupDir:        backupDir,
		SettingsPath:     env("PALWORLD_SETTINGS_PATH", ""),
		SettingsStateDir: env("PALWORLD_SETTINGS_STATE_DIR", "./backups/settings"),
		MetricsInterval:  envDuration("METRICS_INTERVAL", 30*time.Second),
		NetworkTargets:   strings.Split(env("NETWORK_PROBE_TARGETS", "1.1.1.1:53,8.8.8.8:53"), ","),
		NetworkInterval:  envDurationMin("NETWORK_PROBE_INTERVAL", 5*time.Second, time.Second),
		NetworkTimeout:   envDurationMin("NETWORK_PROBE_TIMEOUT", 2*time.Second, 100*time.Millisecond),
		NetworkWindow:    envInt("NETWORK_PROBE_WINDOW", 20),
		NetworkDegraded:  envFloat("NETWORK_DEGRADED_LOSS", 5),
		NetworkCritical:  envFloat("NETWORK_CRITICAL_LOSS", 20),
		ReportDir:        env("REPORT_DIR", filepath.Join(backupDir, "reports")),
		ReportRetention:  envInt("REPORT_RETENTION_DAYS", 30),
		ReportTimezone:   env("REPORT_TIMEZONE", "UTC"),
		ShutdownWait:     envInt("PALWORLD_SHUTDOWN_WAIT", 15),
		ShutdownMessage:  env("PALWORLD_SHUTDOWN_MESSAGE", "Server is shutting down. See you soon."),
	}
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	expected := []byte(a.cfg.PanelPassword)
	provided := []byte(req.Password)
	valid := len(expected) == len(provided) && subtle.ConstantTimeCompare(expected, provided) == 1
	if !valid {
		time.Sleep(350 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "That password is not correct")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "palctrl_session",
		Value:    a.session,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   60 * 60 * 24 * 30,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "palctrl_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type palResult struct {
		state PalState
		err   error
	}
	type dockerResult struct {
		status string
		stats  DockerStats
		err    error
	}

	palCh := make(chan palResult, 1)
	dockerCh := make(chan dockerResult, 1)
	go func() {
		state, err := a.pal.State(ctx)
		palCh <- palResult{state: state, err: err}
	}()
	go func() {
		status, err := a.docker.Status(ctx)
		stats, statsErr := a.docker.Stats(ctx)
		if err == nil {
			err = statsErr
		}
		dockerCh <- dockerResult{status: status, stats: stats, err: err}
	}()

	palRes := <-palCh
	dockerRes := <-dockerCh
	issues := make([]string, 0, 2)
	if palRes.err != nil {
		issues = append(issues, "Game API: "+friendlyError(palRes.err))
	}
	if dockerRes.err != nil && !a.cfg.Mock {
		issues = append(issues, "Docker: "+friendlyError(dockerRes.err))
	}
	network := NetworkHealth{Status: networkStatusDisabled, Targets: []NetworkTargetHealth{}}
	if a.network != nil {
		network = a.network.Health()
		if issue := a.network.Issue(); issue != "" {
			issues = append(issues, issue)
		}
	}

	online := palRes.err == nil
	controlMode := "docker"
	if a.cfg.ControlURL != "" {
		controlMode = "windows"
	}
	if a.cfg.Mock {
		controlMode = "sample"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"online":          online,
		"containerStatus": dockerRes.status,
		"controlMode":     controlMode,
		"info":            palRes.state.Info,
		"metrics":         palRes.state.Metrics,
		"players":         palRes.state.Players,
		"settings":        palRes.state.Settings,
		"host":            dockerRes.stats,
		"network":         network,
		"issues":          issues,
		"sampleMode":      a.cfg.Mock,
		"updatedAt":       time.Now().UTC(),
		"features": map[string]bool{
			"rcon":     a.cfg.RCONPassword != "" || a.cfg.Mock,
			"backup":   a.cfg.SaveDir != "" || a.cfg.Mock,
			"update":   a.cfg.ComposeDir != "" || a.cfg.ControlURL != "" || a.cfg.Mock,
			"settings": a.settingsEditable(),
		},
	})
}

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	a.historyMu.RLock()
	points := append([]Sample(nil), a.history...)
	a.historyMu.RUnlock()
	if a.cfg.Mock && len(points) < 24 {
		points = mockHistory()
	}
	writeJSON(w, http.StatusOK, map[string]any{"samples": points})
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	lines, err := a.docker.Logs(ctx, 240)
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lines":     lines,
		"updatedAt": time.Now().UTC(),
	})
}

func (a *App) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req actionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	var result string
	var err error
	switch req.Action {
	case "start":
		result, err = a.docker.Start(ctx)
		if err == nil && !a.cfg.Mock {
			err = a.waitForPalworld(ctx, 60*time.Second)
		}
	case "restart":
		result, err = a.docker.Restart(ctx)
		if err == nil && !a.cfg.Mock {
			err = a.waitForPalworld(ctx, 60*time.Second)
		}
	case "update":
		result, err = a.docker.Update(ctx)
	case "save":
		err = a.pal.Post(ctx, "/v1/api/save", nil)
		result = "World saved"
	case "shutdown":
		waitTime := req.WaitTime
		if waitTime <= 0 {
			waitTime = a.cfg.ShutdownWait
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = a.cfg.ShutdownMessage
		}
		err = a.pal.Post(ctx, "/v1/api/shutdown", map[string]any{
			"waittime": waitTime,
			"message":  message,
		})
		result = fmt.Sprintf("Shutdown scheduled in %d seconds", waitTime)
		if err == nil && !a.cfg.Mock {
			timer := time.NewTimer(time.Duration(waitTime+2) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
				_, err = a.docker.Stop(ctx)
				if err == nil {
					result = "Server shut down"
				}
			}
		}
	case "force-stop":
		result, err = a.docker.Stop(ctx)
	case "announce":
		if strings.TrimSpace(req.Message) == "" {
			err = errors.New("message is required")
			break
		}
		err = a.pal.Post(ctx, "/v1/api/announce", map[string]any{"message": req.Message})
		result = "Announcement sent"
	case "kick", "ban":
		if strings.TrimSpace(req.UserID) == "" {
			err = errors.New("player user ID is required")
			break
		}
		body := map[string]any{"userid": req.UserID}
		if req.Message != "" {
			body["message"] = req.Message
		}
		err = a.pal.Post(ctx, "/v1/api/"+req.Action, body)
		result = strings.ToUpper(req.Action[:1]) + req.Action[1:] + " request sent"
	case "backup":
		if !a.cfg.Mock {
			_ = a.pal.Post(ctx, "/v1/api/save", nil)
			time.Sleep(1200 * time.Millisecond)
		}
		var backup Backup
		backup, err = createBackup(a.cfg.SaveDir, a.cfg.BackupDir, a.cfg.Mock)
		result = "Backup created: " + backup.Name
	default:
		err = fmt.Errorf("unsupported action %q", req.Action)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": strings.TrimSpace(result)})
}

func (a *App) waitForPalworld(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var info ServerInfo
		lastErr = a.pal.Get(attemptCtx, "/v1/api/info", &info)
		cancel()
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Palworld did not become ready: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (a *App) handleConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req consoleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if len(req.Command) > 500 {
		writeError(w, http.StatusBadRequest, "command is too long")
		return
	}
	if a.cfg.Mock {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"output": "Sample response: command accepted",
		})
		return
	}
	if a.cfg.RCONPassword == "" {
		writeError(w, http.StatusPreconditionFailed, "PALWORLD_RCON_PASSWORD is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	output, err := ExecuteRCON(ctx, a.cfg.RCONAddr, a.cfg.RCONPassword, req.Command)
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": output})
}

func (a *App) handleBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	backups, err := listBackups(a.cfg.BackupDir, a.cfg.Mock)
	if err != nil {
		writeError(w, http.StatusInternalServerError, friendlyError(err))
		return
	}
	var totalSize int64
	for _, backup := range backups {
		totalSize += backup.Size
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"backups":   backups,
		"count":     len(backups),
		"totalSize": totalSize,
	})
}

func (a *App) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "DELETE required")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/backups/")
	deleted, err := deleteBackup(a.cfg.BackupDir, name, a.cfg.Mock)
	switch {
	case errors.Is(err, errInvalidBackupName):
		writeError(w, http.StatusBadRequest, "Invalid backup name")
		return
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "Backup not found")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Backup deleted: " + deleted.Name,
		"deleted": deleted,
	})
}

func (a *App) sampleLoop() {
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	for {
		<-timer.C
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		palState, err := a.pal.State(ctx)
		host, _ := a.docker.Stats(ctx)
		cancel()
		now := time.Now()
		network := NetworkHealth{}
		if a.network != nil {
			network = a.network.Health()
		}
		report := ReportRecord{
			At:            now,
			ServerOnline:  err == nil,
			MemoryUsage:   host.MemoryUsage,
			NetworkStatus: network.Status,
		}
		if network.Enabled && network.Sent > 0 {
			report.PacketLoss = floatPointer(network.PacketLoss)
		}
		if network.Received > 0 {
			report.LatencyMS = floatPointer(network.LatencyMS)
		}
		if err == nil {
			report.FPS = floatPointer(palState.Metrics.ServerFPS)
			report.FrameTimeMS = floatPointer(palState.Metrics.ServerFrameTime)
			report.Players = intPointer(palState.Metrics.CurrentPlayerNum)
			sample := Sample{
				At:          now.UTC(),
				FPS:         palState.Metrics.ServerFPS,
				FrameTime:   palState.Metrics.ServerFrameTime,
				Players:     palState.Metrics.CurrentPlayerNum,
				MemoryUsage: host.MemoryUsage,
				LatencyMS:   network.LatencyMS,
				PacketLoss:  network.PacketLoss,
			}
			a.historyMu.Lock()
			a.history = append(a.history, sample)
			cutoff := time.Now().Add(-24 * time.Hour)
			first := 0
			for first < len(a.history) && a.history[first].At.Before(cutoff) {
				first++
			}
			if first > 0 {
				a.history = append([]Sample(nil), a.history[first:]...)
			}
			a.historyMu.Unlock()
		}
		if a.reports != nil {
			if err := a.reports.Append(report); err != nil {
				a.logReportError(err)
			}
		}
		timer.Reset(a.cfg.MetricsInterval)
	}
}

func (a *App) logReportError(err error) {
	a.reportErrMu.Lock()
	defer a.reportErrMu.Unlock()
	if time.Since(a.reportLastErr) < 5*time.Minute {
		return
	}
	a.reportLastErr = time.Now()
	log.Printf("daily report write failed: %v", err)
}

func floatPointer(value float64) *float64 {
	return &value
}

func intPointer(value int) *int {
	return &value
}

func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("palctrl_session")
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(a.session)) != 1 {
			writeError(w, http.StatusUnauthorized, "Sign in required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.PanelPassword) == "" {
		return errors.New("PANEL_PASSWORD is required; PAL CTRL refuses to start without authentication")
	}
	return nil
}

func (a *App) requireMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Pal-Control") != "1" {
			writeError(w, http.StatusForbidden, "Missing control request header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(r *http.Request, target any) error {
	limited := &io.LimitedReader{R: r.Body, N: 32*1024 + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if limited.N <= 0 {
		return errors.New("request body is too large")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func friendlyError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		message = strings.ReplaceAll(message, cwd, ".")
	}
	return message
}

func randomToken(bytes int) (string, error) {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	return envDurationMin(key, fallback, 5*time.Second)
}

func envDurationMin(key string, fallback, minimum time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || parsed < minimum {
		return fallback
	}
	return parsed
}

func absPath(path string) string {
	if path == "" {
		return ""
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return resolved
}

func mockHistory() []Sample {
	now := time.Now().UTC()
	points := make([]Sample, 96)
	for index := range points {
		fps := 59.1 + 0.65*float64((index%9)-4)/4 + 0.25*float64((index%5)-2)/2
		points[index] = Sample{
			At:          now.Add(time.Duration(index-len(points)+1) * 15 * time.Minute),
			FPS:         fps,
			FrameTime:   1000 / fps,
			Players:     (index / 16) % 4,
			MemoryUsage: "7.82GiB / 16GiB",
			LatencyMS:   34.8 + float64((index%7)-3),
			PacketLoss:  0,
		}
	}
	return points
}
