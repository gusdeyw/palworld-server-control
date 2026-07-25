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
	"strings"
	"sync"
	"time"
)

type ServerInfo struct {
	Version     string `json:"version"`
	ServerName  string `json:"servername"`
	Description string `json:"description"`
	WorldGUID   string `json:"worldguid"`
}

type ServerMetrics struct {
	ServerFPS        float64 `json:"serverfps"`
	CurrentPlayerNum int     `json:"currentplayernum"`
	ServerFrameTime  float64 `json:"serverframetime"`
	MaxPlayerNum     int     `json:"maxplayernum"`
	Uptime           int64   `json:"uptime"`
	Days             int     `json:"days"`
}

type Player struct {
	Name          string  `json:"name"`
	AccountName   string  `json:"accountName"`
	PlayerID      string  `json:"playerId"`
	UserID        string  `json:"userId"`
	IP            string  `json:"ip,omitempty"`
	Ping          float64 `json:"ping"`
	LocationX     float64 `json:"location_x"`
	LocationY     float64 `json:"location_y"`
	Level         int     `json:"level"`
	BuildingCount int     `json:"building_count"`
}

type playerResponse struct {
	Players []Player `json:"players"`
}

type ServerSettings struct {
	Difficulty           string  `json:"Difficulty"`
	DayTimeSpeedRate     float64 `json:"DayTimeSpeedRate"`
	NightTimeSpeedRate   float64 `json:"NightTimeSpeedRate"`
	ExpRate              float64 `json:"ExpRate"`
	PalCaptureRate       float64 `json:"PalCaptureRate"`
	PalSpawnNumRate      float64 `json:"PalSpawnNumRate"`
	DeathPenalty         string  `json:"DeathPenalty"`
	IsPVP                bool    `json:"bIsPvP"`
	FriendlyFire         bool    `json:"bEnableFriendlyFire"`
	ServerPlayerMaxNum   int     `json:"ServerPlayerMaxNum"`
	CrossplayPlatforms   any     `json:"CrossplayPlatforms,omitempty"`
	AllowConnectPlatform string  `json:"AllowConnectPlatform,omitempty"`
	IsUseBackupSaveData  bool    `json:"bIsUseBackupSaveData"`
}

type PalState struct {
	Info     ServerInfo     `json:"info"`
	Metrics  ServerMetrics  `json:"metrics"`
	Players  []Player       `json:"players"`
	Settings ServerSettings `json:"settings"`
}

type PalClient struct {
	baseURL  string
	username string
	password string
	mock     bool
	client   *http.Client
	started  time.Time
}

func NewPalClient(baseURL, username, password string, mock bool) *PalClient {
	return &PalClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		mock:     mock,
		client:   &http.Client{Timeout: 4 * time.Second},
		started:  time.Now().Add(-2*time.Hour - 43*time.Minute),
	}
}

func (p *PalClient) State(ctx context.Context) (PalState, error) {
	if p.mock {
		return p.mockState(), nil
	}
	if p.password == "" {
		return PalState{}, errors.New("PALWORLD_ADMIN_PASSWORD is not configured")
	}

	var state PalState
	var players playerResponse
	type result struct {
		name string
		err  error
	}
	ch := make(chan result, 4)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		ch <- result{"info", p.Get(ctx, "/v1/api/info", &state.Info)}
	}()
	go func() {
		defer wg.Done()
		ch <- result{"metrics", p.Get(ctx, "/v1/api/metrics", &state.Metrics)}
	}()
	go func() {
		defer wg.Done()
		ch <- result{"players", p.Get(ctx, "/v1/api/players", &players)}
	}()
	go func() {
		defer wg.Done()
		ch <- result{"settings", p.Get(ctx, "/v1/api/settings", &state.Settings)}
	}()
	go func() {
		wg.Wait()
		close(ch)
	}()

	var essentialErr error
	for item := range ch {
		if item.err != nil && (item.name == "metrics" || item.name == "info") {
			essentialErr = fmt.Errorf("%s: %w", item.name, item.err)
		}
	}
	state.Players = players.Players
	if state.Players == nil {
		state.Players = []Player{}
	}
	return state, essentialErr
}

func (p *PalClient) Get(ctx context.Context, path string, target any) error {
	return p.do(ctx, http.MethodGet, path, nil, target)
}

func (p *PalClient) Post(ctx context.Context, path string, body any) error {
	if p.mock {
		return nil
	}
	if p.password == "" {
		return errors.New("PALWORLD_ADMIN_PASSWORD is not configured")
	}
	return p.do(ctx, http.MethodPost, path, body, nil)
}

func (p *PalClient) do(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.username, p.password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		message := strings.TrimSpace(string(data))
		if message == "" {
			message = response.Status
		}
		return fmt.Errorf("%s", message)
	}
	if target == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (p *PalClient) mockState() PalState {
	elapsed := time.Since(p.started)
	phase := float64(time.Now().Unix()%300) / 300 * math.Pi * 2
	fps := 59.2 + math.Sin(phase)*0.7
	players := []Player{
		{Name: "Wina", AccountName: "wina", UserID: "steam_sample_wina", PlayerID: "sample_wina", Ping: 34, Level: 48, BuildingCount: 114},
		{Name: "Raka", AccountName: "raka", UserID: "steam_sample_raka", PlayerID: "sample_raka", Ping: 51, Level: 42, BuildingCount: 87},
		{Name: "Miko", AccountName: "miko", UserID: "steam_sample_miko", PlayerID: "sample_miko", Ping: 62, Level: 39, BuildingCount: 71},
	}
	return PalState{
		Info: ServerInfo{
			Version:     "1.0.0",
			ServerName:  "Palpagos After Hours",
			Description: "Private world for four friends",
			WorldGUID:   "SAMPLE-WORLD",
		},
		Metrics: ServerMetrics{
			ServerFPS:        math.Round(fps*10) / 10,
			CurrentPlayerNum: len(players),
			ServerFrameTime:  math.Round((1000/fps)*100) / 100,
			MaxPlayerNum:     4,
			Uptime:           int64(elapsed.Seconds()),
			Days:             128,
		},
		Players: players,
		Settings: ServerSettings{
			Difficulty:           "None",
			DayTimeSpeedRate:     1,
			NightTimeSpeedRate:   1,
			ExpRate:              1.5,
			PalCaptureRate:       1.2,
			PalSpawnNumRate:      1,
			DeathPenalty:         "Item",
			ServerPlayerMaxNum:   4,
			IsUseBackupSaveData:  true,
			AllowConnectPlatform: "Steam,Xbox,PS5,Mac",
		},
	}
}
