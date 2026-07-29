package main

import (
	"reflect"
	"testing"
)

func TestFilterPalworldLogLinesRemovesRESTPollingAndFormatsGameEvents(t *testing.T) {
	raw := []string{
		`2026-07-29T04:30:31.000000000Z {`,
		`2026-07-29T04:30:31.000000000Z   "timestamp": "2026-07-29 04:30:31",`,
		`2026-07-29T04:30:31.000000000Z   "event": "command",`,
		`2026-07-29T04:30:31.000000000Z   "playername": "REST",`,
		`2026-07-29T04:30:31.000000000Z   "details": ["/v1/api/players", "OK"]`,
		`2026-07-29T04:30:31.000000000Z }`,
		`2026-07-29T04:48:43.000000000Z {`,
		`2026-07-29T04:48:43.000000000Z   "timestamp": "2026-07-29 04:48:43",`,
		`2026-07-29T04:48:43.000000000Z   "event": "left",`,
		`2026-07-29T04:48:43.000000000Z   "playername": "PakMahmad",`,
		`2026-07-29T04:48:43.000000000Z   "details": []`,
		`2026-07-29T04:48:43.000000000Z }`,
	}

	got := filterPalworldLogLines(raw, 240)
	want := []string{"[2026-07-29 04:48:43] LEFT · PakMahmad"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected filtered logs:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFilterPalworldLogLinesKeepsUnstructuredServerOutput(t *testing.T) {
	raw := []string{
		"2026-07-29T04:00:00Z Game version is v1.0.2.100933",
		"2026-07-29T04:00:01Z Running Palworld dedicated server on :8211",
	}

	got := filterPalworldLogLines(raw, 240)
	want := []string{
		"[2026-07-29 04:00:00] Game version is v1.0.2.100933",
		"[2026-07-29 04:00:01] Running Palworld dedicated server on :8211",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected server output:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFilterPalworldLogLinesAppliesLimitAfterFiltering(t *testing.T) {
	raw := []string{
		"2026-07-29T04:00:00Z first",
		"2026-07-29T04:00:01Z second",
		"2026-07-29T04:00:02Z third",
	}

	got := filterPalworldLogLines(raw, 2)
	want := []string{
		"[2026-07-29 04:00:01] second",
		"[2026-07-29 04:00:02] third",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected limited output:\n got: %#v\nwant: %#v", got, want)
	}
}
