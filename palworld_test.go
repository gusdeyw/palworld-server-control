package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPalClientState(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/info", func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "admin" || password != "secret" {
			t.Fatal("expected Palworld basic authentication")
		}
		_ = json.NewEncoder(w).Encode(ServerInfo{Version: "1.0.0", ServerName: "Test World"})
	})
	mux.HandleFunc("/v1/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServerMetrics{ServerFPS: 59.5, CurrentPlayerNum: 1, MaxPlayerNum: 4})
	})
	mux.HandleFunc("/v1/api/players", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(playerResponse{Players: []Player{{Name: "Wina", UserID: "steam_wina"}}})
	})
	mux.HandleFunc("/v1/api/settings", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ServerSettings{ExpRate: 1.5, ServerPlayerMaxNum: 4})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewPalClient(server.URL, "admin", "secret", false)
	state, err := client.State(context.Background())
	if err != nil {
		t.Fatalf("State returned an error: %v", err)
	}
	if state.Info.ServerName != "Test World" {
		t.Fatalf("unexpected server name %q", state.Info.ServerName)
	}
	if state.Metrics.ServerFPS != 59.5 {
		t.Fatalf("unexpected FPS %.1f", state.Metrics.ServerFPS)
	}
	if len(state.Players) != 1 || state.Players[0].Name != "Wina" {
		t.Fatalf("unexpected players: %#v", state.Players)
	}
}
