package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReportHandlersListReadAndDownload(t *testing.T) {
	store, err := NewReportStore(t.TempDir(), 30, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	latency, loss := 3.2, 0.0
	if err := store.Append(ReportRecord{
		At:            time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC),
		ServerOnline:  true,
		LatencyMS:     &latency,
		PacketLoss:    &loss,
		NetworkStatus: networkStatusHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	app := &App{reports: store}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/reports", nil)
	listResponse := httptest.NewRecorder()
	app.handleReports(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var list struct {
		Reports       []ReportSummary `json:"reports"`
		Timezone      string          `json:"timezone"`
		RetentionDays int             `json:"retentionDays"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Reports) != 1 || list.Reports[0].Date != "2026-07-27" {
		t.Fatalf("unexpected report list: %#v", list)
	}
	if list.Timezone != "UTC" || list.RetentionDays != 30 {
		t.Fatalf("unexpected report configuration: %#v", list)
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/reports/2026-07-27", nil)
	detailResponse := httptest.NewRecorder()
	app.handleReports(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail returned %d: %s", detailResponse.Code, detailResponse.Body.String())
	}

	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/reports/2026-07-27/download", nil)
	downloadResponse := httptest.NewRecorder()
	app.handleReports(downloadResponse, downloadRequest)
	if downloadResponse.Code != http.StatusOK {
		t.Fatalf("download returned %d: %s", downloadResponse.Code, downloadResponse.Body.String())
	}
	if !strings.HasPrefix(downloadResponse.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("unexpected content type: %s", downloadResponse.Header().Get("Content-Type"))
	}
	if !strings.Contains(downloadResponse.Header().Get("Content-Disposition"), "palctrl-report-2026-07-27.csv") {
		t.Fatalf("unexpected disposition: %s", downloadResponse.Header().Get("Content-Disposition"))
	}
	if !strings.HasPrefix(downloadResponse.Body.String(), "timestamp,server_online") {
		t.Fatalf("unexpected CSV body: %q", downloadResponse.Body.String())
	}
}

func TestReportHandlerRejectsTraversal(t *testing.T) {
	store, err := NewReportStore(t.TempDir(), 30, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{reports: store}
	request := httptest.NewRequest(http.MethodGet, "/api/reports/not-a-date", nil)
	response := httptest.NewRecorder()
	app.handleReports(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}
