package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReportStoreWritesDailyCSVAndSummaries(t *testing.T) {
	store, err := NewReportStore(t.TempDir(), 30, "Asia/Makassar")
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 7, 27, 0, 5, 0, 0, time.FixedZone("WITA", 8*60*60))
	second := first.Add(50 * time.Minute)
	fpsOne, fpsTwo := 59.0, 57.0
	playersOne, playersTwo := 1, 3
	latencyOne, latencyTwo := 35.0, 45.0
	lossOne, lossTwo := 0.0, 25.0

	if err := store.Append(ReportRecord{
		At: first, ServerOnline: true, FPS: &fpsOne, Players: &playersOne,
		LatencyMS: &latencyOne, PacketLoss: &lossOne, NetworkStatus: networkStatusHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ReportRecord{
		At: second, ServerOnline: true, FPS: &fpsTwo, Players: &playersTwo,
		LatencyMS: &latencyTwo, PacketLoss: &lossTwo, NetworkStatus: networkStatusCritical,
	}); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(store.dir, "2026-07-27.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(file).ReadAll()
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected header and two samples, got %d rows", len(rows))
	}
	if !equalStrings(rows[0], reportHeader) {
		t.Fatalf("unexpected CSV header: %#v", rows[0])
	}

	report, err := store.Read("2026-07-27")
	if err != nil {
		t.Fatal(err)
	}
	summary := report.Summary
	if summary.Samples != 2 || summary.OnlinePercent != 100 {
		t.Fatalf("unexpected coverage summary: %#v", summary)
	}
	if summary.AverageFPS != 58 || summary.MinimumFPS != 57 || summary.PeakPlayers != 3 {
		t.Fatalf("unexpected game summary: %#v", summary)
	}
	if summary.AverageLatency != 40 || summary.MaximumLatency != 45 {
		t.Fatalf("unexpected latency summary: %#v", summary)
	}
	if summary.AverageLoss != 12.5 || summary.MaximumLoss != 25 {
		t.Fatalf("unexpected loss summary: %#v", summary)
	}
	if summary.Status != networkStatusCritical || summary.CriticalSamples != 1 {
		t.Fatalf("unexpected network status summary: %#v", summary)
	}
	if len(report.Hours) != 1 || report.Hours[0].Hour != "00:00" {
		t.Fatalf("unexpected hourly report: %#v", report.Hours)
	}
}

func TestReportStoreUsesConfiguredCalendarTimezone(t *testing.T) {
	store, err := NewReportStore(t.TempDir(), 30, "Asia/Makassar")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 26, 17, 30, 0, 0, time.UTC)
	if err := store.Append(ReportRecord{At: at, NetworkStatus: networkStatusDisabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.dir, "2026-07-27.csv")); err != nil {
		t.Fatalf("expected sample in WITA calendar day: %v", err)
	}
	report, err := store.Read("2026-07-27")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Status != networkStatusDisabled {
		t.Fatalf("expected disabled network status, got %q", report.Summary.Status)
	}
}

func TestReportStoreRetentionAndDateValidation(t *testing.T) {
	store, err := NewReportStore(t.TempDir(), 2, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	for day := 1; day <= 3; day++ {
		at := time.Date(2026, 7, day, 12, 0, 0, 0, time.UTC)
		if err := store.Append(ReportRecord{At: at, NetworkStatus: networkStatusHealthy}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(store.dir, "2026-07-01.csv")); !os.IsNotExist(err) {
		t.Fatalf("expected expired report to be removed, got %v", err)
	}
	for _, date := range []string{"../2026-07-03", "2026-7-3", "2026-02-30", ""} {
		if _, err := store.Read(date); err == nil {
			t.Fatalf("expected invalid date %q to be rejected", date)
		}
	}
}

func TestReportStoreRecordsOfflineSamplesWithoutGameMetrics(t *testing.T) {
	store, err := NewReportStore(t.TempDir(), 30, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	loss := 100.0
	if err := store.Append(ReportRecord{
		At:            time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC),
		ServerOnline:  false,
		PacketLoss:    &loss,
		NetworkStatus: networkStatusCritical,
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.Read("2026-07-27")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Samples != 1 || report.Summary.OnlinePercent != 0 {
		t.Fatalf("unexpected offline summary: %#v", report.Summary)
	}
	if report.Summary.AverageFPS != 0 || report.Summary.MaximumLoss != 100 {
		t.Fatalf("unexpected offline metrics: %#v", report.Summary)
	}
}
