package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGameSettingCatalogIsCompleteAndValid(t *testing.T) {
	definitions := gameSettingDefinitions()
	if len(definitions) < 90 {
		t.Fatalf("expected an exhaustive official settings catalog, got %d definitions", len(definitions))
	}
	seen := make(map[string]bool)
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Key) == "" {
			t.Fatal("setting definition has an empty key")
		}
		if seen[definition.Key] {
			t.Fatalf("duplicate setting definition %q", definition.Key)
		}
		seen[definition.Key] = true
		if _, err := normalizeSettingValue(definition, definition.Default); err != nil {
			t.Fatalf("invalid default for %s: %v", definition.Key, err)
		}
	}
	for _, preset := range gamePresets() {
		if preset.Baseline {
			continue
		}
		if _, err := validateSettingChanges(preset.Changes); err != nil {
			t.Fatalf("invalid preset %s: %v", preset.ID, err)
		}
	}
}

func TestApplyINIChangesPreservesInfrastructureAndNestedValues(t *testing.T) {
	input := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Friends, Inc.",AdminPassword="test-admin-secret",ServerPassword="test-game-secret",CrossplayPlatforms=(Steam,Xbox,PS5,Mac),DenyTechnologyList=("PALBOX","RepairBench"),PlayerDamageRateAttack=1.000000,DeathPenalty=Item)
`
	updated, err := applyINIChanges(input, map[string]any{
		"PlayerDamageRateAttack": 2.5,
		"DeathPenalty":           "None",
		"ExpRate":                3.0,
	})
	if err != nil {
		t.Fatalf("applyINIChanges returned an error: %v", err)
	}
	for _, expected := range []string{
		`ServerName="Friends, Inc."`,
		`AdminPassword="test-admin-secret"`,
		`ServerPassword="test-game-secret"`,
		`CrossplayPlatforms=(Steam,Xbox,PS5,Mac)`,
		`DenyTechnologyList=("PALBOX","RepairBench")`,
		`PlayerDamageRateAttack=2.500000`,
		`DeathPenalty=None`,
		`ExpRate=3.000000`,
	} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated INI does not contain %q:\n%s", expected, updated)
		}
	}
	if _, _, _, err := parseOptionSettings(updated); err != nil {
		t.Fatalf("updated INI could not be parsed: %v", err)
	}
}

func TestParseINIValuesUsesDefaultsForMissingSettings(t *testing.T) {
	input := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ExpRate=2.500000,bEnableFriendlyFire=True,DenyTechnologyList=("PALBOX","RepairBench"))
`
	values, err := parseINIValues(input)
	if err != nil {
		t.Fatalf("parseINIValues returned an error: %v", err)
	}
	if values["ExpRate"] != 2.5 {
		t.Fatalf("unexpected ExpRate: %#v", values["ExpRate"])
	}
	if values["bEnableFriendlyFire"] != true {
		t.Fatalf("unexpected friendly-fire value: %#v", values["bEnableFriendlyFire"])
	}
	list, ok := values["DenyTechnologyList"].([]string)
	if !ok || len(list) != 2 || list[0] != "PALBOX" || list[1] != "RepairBench" {
		t.Fatalf("unexpected technology list: %#v", values["DenyTechnologyList"])
	}
	if values["PlayerDamageRateAttack"] != float64(1) {
		t.Fatalf("expected missing values to use official defaults, got %#v", values["PlayerDamageRateAttack"])
	}
}

func TestValidateSettingChangesRejectsUnknownAndUnsafeValues(t *testing.T) {
	if _, err := validateSettingChanges(map[string]any{"AdminPassword": "changed"}); err == nil {
		t.Fatal("expected infrastructure settings to be rejected")
	}
	if _, err := validateSettingChanges(map[string]any{"PlayerDamageRateAttack": 99.0}); err == nil {
		t.Fatal("expected an out-of-range multiplier to be rejected")
	}
	if _, err := validateSettingChanges(map[string]any{"DenyTechnologyList": []any{"PALBOX", "bad,value"}}); err == nil {
		t.Fatal("expected unsafe list punctuation to be rejected")
	}
}

func TestMergeEffectiveSettingsSupportsPalworldAPIKeyCasing(t *testing.T) {
	raw := map[string]any{
		"autoSaveSpan": 45.0,
		"ExpRate":      1.75,
	}
	values := mergeEffectiveSettings(raw)
	if values["AutoSaveSpan"] != 45.0 {
		t.Fatalf("unexpected autosave value: %#v", values["AutoSaveSpan"])
	}
	if values["ExpRate"] != 1.75 {
		t.Fatalf("unexpected experience value: %#v", values["ExpRate"])
	}
}

func TestBaselineJSONRoundTripValidatesAllDefaults(t *testing.T) {
	data, err := json.Marshal(officialSettingDefaults())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSettingChanges(decoded); err != nil {
		t.Fatalf("defaults did not survive a JSON round trip: %v", err)
	}
}
