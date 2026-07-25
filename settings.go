package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type SettingDefinition struct {
	Key         string          `json:"key"`
	APIKey      string          `json:"apiKey,omitempty"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Group       string          `json:"group"`
	Type        string          `json:"type"`
	Unit        string          `json:"unit,omitempty"`
	Min         *float64        `json:"min,omitempty"`
	Max         *float64        `json:"max,omitempty"`
	Step        *float64        `json:"step,omitempty"`
	Default     any             `json:"default"`
	Options     []SettingOption `json:"options,omitempty"`
	Warning     string          `json:"warning,omitempty"`
	BareString  bool            `json:"-"`
}

type SettingGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type GamePreset struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Tone        string         `json:"tone"`
	Baseline    bool           `json:"baseline,omitempty"`
	Changes     map[string]any `json:"changes"`
}

type settingsApplyRequest struct {
	Preset  string         `json:"preset"`
	Changes map[string]any `json:"changes"`
}

type settingsResponse struct {
	Groups            []SettingGroup      `json:"groups"`
	Definitions       []SettingDefinition `json:"definitions"`
	Presets           []GamePreset        `json:"presets"`
	Values            map[string]any      `json:"values"`
	Editable          bool                `json:"editable"`
	RollbackAvailable bool                `json:"rollbackAvailable"`
	Source            string              `json:"source"`
	UpdatedAt         time.Time           `json:"updatedAt"`
}

type iniEntry struct {
	Key   string
	Value string
}

func number(value float64) *float64 {
	return &value
}

func gameSettingGroups() []SettingGroup {
	return []SettingGroup{
		{ID: "combat", Name: "Combat", Description: "Damage, durability, death, raids and boss difficulty."},
		{ID: "progression", Name: "Progression", Description: "Experience, capture, breeding, work and technology rules."},
		{ID: "survival", Name: "Survival", Description: "Time, hunger, stamina, regeneration, weight and item decay."},
		{ID: "world", Name: "World", Description: "Spawns, travel, randomization, invasions and world events."},
		{ID: "resources", Name: "Resources", Description: "Gathering, drops, respawn timing and ranch production."},
		{ID: "bases", Name: "Bases", Description: "Base limits, workers, buildings, guild cleanup and transfers."},
		{ID: "players", Name: "Players", Description: "Player capacity, PvP, chat, voice, visibility and social rules."},
		{ID: "performance", Name: "Performance", Description: "Autosaves, replication and object limits that affect server load."},
	}
}

func gameSettingDefinitions() []SettingDefinition {
	rate := func(key, label, description, group string, defaultValue float64) SettingDefinition {
		return SettingDefinition{
			Key: key, Label: label, Description: description, Group: group,
			Type: "number", Unit: "x", Min: number(0), Max: number(10), Step: number(0.1), Default: defaultValue,
		}
	}
	integer := func(key, label, description, group string, defaultValue int, min, max int) SettingDefinition {
		return SettingDefinition{
			Key: key, Label: label, Description: description, Group: group,
			Type: "integer", Min: number(float64(min)), Max: number(float64(max)), Step: number(1), Default: defaultValue,
		}
	}
	toggle := func(key, label, description, group string, defaultValue bool) SettingDefinition {
		return SettingDefinition{
			Key: key, Label: label, Description: description, Group: group,
			Type: "boolean", Default: defaultValue,
		}
	}

	return []SettingDefinition{
		{Key: "Difficulty", Label: "Difficulty profile", Description: "Base difficulty profile used before individual multipliers.", Group: "combat", Type: "select", Default: "None", BareString: true, Options: []SettingOption{{Value: "None", Label: "Custom / None"}, {Value: "Normal", Label: "Normal"}, {Value: "Hard", Label: "Hard"}}},
		rate("PlayerDamageRateAttack", "Player damage dealt", "Multiplier for damage dealt directly by players.", "combat", 1),
		rate("PlayerDamageRateDefense", "Player damage received", "Multiplier for damage taken by players. Lower values make players tougher.", "combat", 1),
		rate("PalDamageRateAttack", "Pal damage dealt", "Multiplier for damage dealt by all Pals, including hostile Pals and bosses.", "combat", 1),
		rate("PalDamageRateDefense", "Pal damage received", "Multiplier for damage taken by all Pals, including hostile Pals and bosses.", "combat", 1),
		rate("EquipmentDurabilityDamageRate", "Equipment durability loss", "Multiplier for durability damage to equipped items.", "combat", 1),
		{Key: "DeathPenalty", Label: "Death penalty", Description: "Controls what a player drops after dying.", Group: "combat", Type: "select", Default: "Item", BareString: true, Options: []SettingOption{{Value: "None", Label: "Nothing"}, {Value: "Item", Label: "Items only"}, {Value: "ItemAndEquipment", Label: "Items and equipment"}, {Value: "All", Label: "Items, equipment and team Pals"}}},
		toggle("bHardcore", "Hardcore mode", "Prevents normal respawning after death.", "combat", false),
		toggle("bPalLost", "Lose Pals on death", "Permanently removes Pals when the character dies.", "combat", false),
		toggle("bCharacterRecreateInHardcore", "Hardcore character recreation", "Allows creating a replacement character after a hardcore death.", "combat", false),
		{Key: "BlockRespawnTime", Label: "Respawn delay", Description: "Base delay before a player can respawn.", Group: "combat", Type: "number", Unit: "sec", Min: number(0), Max: number(3600), Step: number(1), Default: float64(5)},
		{Key: "RespawnPenaltyDurationThreshold", Label: "Respawn penalty threshold", Description: "Survival time used when deciding whether the next respawn penalty applies.", Group: "combat", Type: "number", Unit: "sec", Min: number(0), Max: number(86400), Step: number(1), Default: float64(0)},
		rate("RespawnPenaltyTimeScale", "Respawn penalty scale", "Multiplier applied to the respawn cooldown after the threshold is met.", "combat", 2),
		toggle("EnablePredatorBossPal", "Predator bosses", "Allows predator boss Pals to appear in the world.", "combat", true),
		toggle("bEnableInvaderEnemy", "Base invasions", "Allows hostile invasion events against bases.", "combat", true),
		toggle("bActiveUNKO", "UNKO activity", "Enables the UNKO world activity flag provided by Palworld.", "combat", false),

		rate("ExpRate", "Experience gain", "Multiplier for experience earned by players and Pals.", "progression", 1),
		rate("PalCaptureRate", "Pal capture rate", "Multiplier for the probability of successfully capturing Pals.", "progression", 1),
		{Key: "PalEggDefaultHatchingTime", Label: "Huge egg incubation", Description: "Hours required to hatch a Huge Egg. Other egg times scale from this value.", Group: "progression", Type: "number", Unit: "hours", Min: number(0), Max: number(240), Step: number(0.1), Default: float64(1)},
		rate("WorkSpeedRate", "Work speed", "Global multiplier for crafting and work speed.", "progression", 1),
		rate("MonsterFarmActionSpeedRate", "Ranch production speed", "Multiplier for item production from grazing Pals.", "progression", 1),
		toggle("bAllowEnhanceStat_Health", "Allow Health upgrades", "Allows players to spend stat points on Health.", "progression", true),
		toggle("bAllowEnhanceStat_Attack", "Allow Attack upgrades", "Allows players to spend stat points on Attack.", "progression", true),
		toggle("bAllowEnhanceStat_Stamina", "Allow Stamina upgrades", "Allows players to spend stat points on Stamina.", "progression", true),
		toggle("bAllowEnhanceStat_Weight", "Allow Weight upgrades", "Allows players to spend stat points on carrying capacity.", "progression", true),
		toggle("bAllowEnhanceStat_WorkSpeed", "Allow Work Speed upgrades", "Allows players to spend stat points on Work Speed.", "progression", true),
		{Key: "DenyTechnologyList", Label: "Disabled technologies", Description: "Comma-separated official technology IDs that players cannot unlock.", Group: "progression", Type: "list", Default: []string{}, Warning: "Invalid technology IDs are ignored by the game."},

		rate("DayTimeSpeedRate", "Day speed", "Multiplier for how quickly daytime passes.", "survival", 1),
		rate("NightTimeSpeedRate", "Night speed", "Multiplier for how quickly nighttime passes.", "survival", 1),
		rate("PlayerStomachDecreaceRate", "Player hunger drain", "Multiplier for player hunger depletion. Lower values drain more slowly.", "survival", 1),
		rate("PlayerStaminaDecreaceRate", "Player stamina drain", "Multiplier for player stamina depletion. Lower values drain more slowly.", "survival", 1),
		rate("PlayerAutoHPRegeneRate", "Player health regeneration", "Multiplier for natural player health regeneration.", "survival", 1),
		rate("PlayerAutoHpRegeneRateInSleep", "Player sleep regeneration", "Multiplier for player health regeneration while sleeping.", "survival", 1),
		rate("PalStomachDecreaceRate", "Pal hunger drain", "Multiplier for Pal hunger depletion. Lower values drain more slowly.", "survival", 1),
		rate("PalStaminaDecreaceRate", "Pal stamina drain", "Multiplier for Pal stamina depletion. Lower values drain more slowly.", "survival", 1),
		rate("PalAutoHPRegeneRate", "Pal health regeneration", "Multiplier for natural Pal health regeneration.", "survival", 1),
		rate("PalAutoHpRegeneRateInSleep", "Palbox health regeneration", "Multiplier for Pal health regeneration while resting in the Palbox.", "survival", 1),
		rate("ItemWeightRate", "Item weight", "Multiplier applied to item weight.", "survival", 1),
		rate("ItemCorruptionMultiplier", "Item spoilage speed", "Multiplier for food and item corruption speed.", "survival", 1),

		{Key: "RandomizerType", Label: "Pal randomizer", Description: "Controls whether Pal spawns are randomized by region or across the whole world.", Group: "world", Type: "select", Default: "None", BareString: true, Options: []SettingOption{{Value: "None", Label: "Disabled"}, {Value: "Region", Label: "Within each region"}, {Value: "All", Label: "Entire world"}}},
		{Key: "RandomizerSeed", Label: "Randomizer seed", Description: "Seed used for randomized Pal spawns.", Group: "world", Type: "text", Default: ""},
		toggle("bIsRandomizerPalLevelRandom", "Fully random Pal levels", "Randomizes wild Pal levels instead of keeping them near each area's intended range.", "world", false),
		rate("PalSpawnNumRate", "Pal spawn amount", "Multiplier for Pal population. Higher values increase CPU and memory use.", "world", 1),
		toggle("bEnableFastTravel", "Fast travel", "Allows players to use fast travel.", "world", true),
		toggle("bEnableFastTravelOnlyBaseCamp", "Base-only fast travel", "Restricts fast travel to travel between bases.", "world", false),
		toggle("bIsStartLocationSelectByMap", "Choose starting location", "Allows players to select a starting point from the map.", "world", false),
		{Key: "SupplyDropSpan", Label: "Supply drop interval", Description: "Minutes between meteorite and supply-drop events.", Group: "world", Type: "integer", Unit: "min", Min: number(1), Max: number(10080), Step: number(1), Default: 180},

		rate("CollectionDropRate", "Gatherable item yield", "Multiplier for items received from gatherable objects.", "resources", 1),
		rate("CollectionObjectHpRate", "Gatherable object health", "Multiplier for the health of rocks, trees and other gatherable objects.", "resources", 1),
		rate("CollectionObjectRespawnSpeedRate", "Gatherable respawn interval", "Multiplier for gatherable-object respawn time. Lower values respawn faster.", "resources", 1),
		rate("EnemyDropItemRate", "Enemy drop quantity", "Multiplier for item quantities dropped by enemies.", "resources", 1),

		integer("BaseCampMaxNum", "World base limit", "Maximum total number of bases across the server.", "bases", 128, 1, 1024),
		integer("BaseCampMaxNumInGuild", "Bases per guild", "Maximum number of bases a guild may own.", "bases", 4, 1, 10),
		integer("BaseCampWorkerMaxNum", "Workers per base", "Maximum number of working Pals at each base.", "bases", 15, 1, 50),
		integer("MaxBuildingLimitNum", "Buildings per player", "Maximum buildings per player. Zero means unlimited.", "bases", 0, 0, 1000000),
		rate("BuildObjectHpRate", "Building health", "Multiplier for building hit points.", "bases", 1),
		rate("BuildObjectDamageRate", "Building damage received", "Multiplier for damage dealt to buildings.", "bases", 1),
		rate("BuildObjectDeteriorationDamageRate", "Building deterioration", "Multiplier for natural building decay.", "bases", 1),
		toggle("bBuildAreaLimit", "Building area restrictions", "Prevents building near protected structures such as fast-travel points.", "bases", false),
		toggle("bEnableNonLoginPenalty", "Offline base penalties", "Allows penalties that apply while guild members are offline.", "bases", true),
		toggle("bAutoResetGuildNoOnlinePlayers", "Delete abandoned guild bases", "Automatically resets guild structures after the configured offline period.", "bases", false),
		{Key: "AutoResetGuildTimeNoOnlinePlayers", Label: "Abandoned guild timeout", Description: "Offline hours before abandoned-guild cleanup can trigger.", Group: "bases", Type: "number", Unit: "hours", Min: number(1), Max: number(8760), Step: number(1), Default: float64(72)},
		toggle("bExistPlayerAfterLogout", "Keep sleeping players", "Leaves player characters sleeping in the world after logout.", "bases", false),
		toggle("bEnableDefenseOtherGuildPlayer", "Defend against other guilds", "Enables the other-guild player defense behavior.", "bases", false),
		toggle("bInvisibleOtherGuildBaseCampAreaFX", "Hide other guild boundaries", "Hides base-area boundary effects belonging to other guilds.", "bases", false),
		toggle("bAllowGlobalPalboxExport", "Global Palbox export", "Allows players to export Pals to the Global Palbox.", "bases", true),
		toggle("bAllowGlobalPalboxImport", "Global Palbox import", "Allows players to import Pals from the Global Palbox.", "bases", false),
		integer("GuildRejoinCooldownMinutes", "Guild rejoin cooldown", "Minutes a player must wait before rejoining a guild.", "bases", 0, 0, 10080),
		integer("AutoTransferMasterCheckIntervalSeconds", "Guild master check interval", "Seconds between automatic guild-master transfer checks.", "bases", 3600, 30, 604800),
		integer("AutoTransferMasterThresholdDays", "Guild master transfer threshold", "Offline days before guild-master transfer eligibility.", "bases", 14, 1, 3650),

		integer("ServerPlayerMaxNum", "Server player limit", "Maximum simultaneous players on this dedicated server.", "players", 32, 1, 32),
		integer("CoopPlayerMaxNum", "Co-op player limit", "Maximum players used by co-op sessions.", "players", 4, 1, 32),
		integer("GuildPlayerMaxNum", "Guild member limit", "Maximum number of players in one guild.", "players", 20, 1, 100),
		toggle("bIsPvP", "PvP", "Allows players to fight other players.", "players", false),
		toggle("bEnablePlayerToPlayerDamage", "Player-to-player damage", "Allows direct damage between player characters.", "players", false),
		toggle("bEnableFriendlyFire", "Friendly fire", "Allows attacks to damage allies.", "players", false),
		toggle("bCanPickupOtherGuildDeathPenaltyDrop", "Loot other guild death drops", "Allows players to collect death-penalty drops belonging to another guild.", "players", false),
		toggle("bShowPlayerList", "Show player list", "Displays the connected-player list in the Escape menu.", "players", false),
		toggle("bIsShowJoinLeftMessage", "Join and leave messages", "Shows in-game messages when players connect or disconnect.", "players", true),
		integer("ChatPostLimitPerMinute", "Chat messages per minute", "Rate limit for each player's chat messages.", "players", 30, 1, 600),
		toggle("bAllowClientMod", "Allow modded clients", "Allows clients with enabled mods to join.", "players", true),
		toggle("bEnableVoiceChat", "Voice chat", "Enables Palworld's in-game voice chat.", "players", false),
		{Key: "VoiceChatMaxVolumeDistance", Label: "Voice full-volume distance", Description: "Distance in centimeters before voice chat starts fading.", Group: "players", Type: "number", Unit: "cm", Min: number(0), Max: number(100000), Step: number(100), Default: float64(3000)},
		{Key: "VoiceChatZeroVolumeDistance", Label: "Voice cutoff distance", Description: "Distance in centimeters at which voice chat becomes silent.", Group: "players", Type: "number", Unit: "cm", Min: number(0), Max: number(200000), Step: number(100), Default: float64(15000)},
		toggle("bDisplayPvPItemNumOnWorldMap_BaseCamp", "Show PvP items at bases", "Displays the number of PvP-exclusive items at bases on the world map.", "players", false),
		toggle("bDisplayPvPItemNumOnWorldMap_Player", "Show PvP items on players", "Displays player locations and PvP-exclusive item counts on the world map.", "players", false),
		toggle("bAdditionalDropItemWhenPlayerKillingInPvPMode", "PvP bonus item drop", "Drops an additional configured item when a player kills another player in PvP.", "players", false),
		{Key: "AdditionalDropItemWhenPlayerKillingInPvPMode", Label: "PvP bonus item ID", Description: "Official item ID used by the PvP bonus-drop rule.", Group: "players", Type: "text", Default: "PlayerDropItem"},
		integer("AdditionalDropItemNumWhenPlayerKillingInPvPMode", "PvP bonus item quantity", "Quantity of the configured bonus item dropped after a PvP kill.", "players", 1, 1, 9999),
		toggle("bEnableBuildingPlayerUIdDisplay", "Show building creator IDs", "Displays the creator's player ID on structures.", "players", false),

		{Key: "AutoSaveSpan", APIKey: "autoSaveSpan", Label: "Autosave interval", Description: "Seconds between Palworld automatic world saves.", Group: "performance", Type: "number", Unit: "sec", Min: number(10), Max: number(3600), Step: number(5), Default: float64(30)},
		toggle("bIsUseBackupSaveData", "Native rolling backups", "Keeps Palworld's own rolling save-data backups enabled.", "performance", true),
		integer("DropItemMaxNum", "World dropped-item limit", "Maximum dropped items retained in the world.", "performance", 3000, 0, 100000),
		integer("PhysicsActiveDropItemMaxNum", "Physics-active item limit", "Maximum dropped items with active physics. Minus one uses Palworld's automatic behavior.", "performance", -1, -1, 100000),
		integer("DropItemMaxNum_UNKO", "UNKO dropped-item limit", "Maximum UNKO dropped objects retained in the world.", "performance", 100, 0, 100000),
		{Key: "DropItemAliveMaxHours", Label: "Dropped-item lifetime", Description: "Hours dropped items remain before cleanup.", Group: "performance", Type: "number", Unit: "hours", Min: number(0), Max: number(720), Step: number(0.1), Default: float64(1)},
		{Key: "ServerReplicatePawnCullDistance", Label: "Replication distance", Description: "Maximum Pal replication distance in centimeters.", Group: "performance", Type: "number", Unit: "cm", Min: number(5000), Max: number(15000), Step: number(100), Default: float64(15000)},
		{Key: "ItemContainerForceMarkDirtyInterval", Label: "Container resync interval", Description: "Seconds between forced container synchronization while its UI is open.", Group: "performance", Type: "number", Unit: "sec", Min: number(0.1), Max: number(60), Step: number(0.1), Default: float64(1)},
		{Key: "PlayerDataPalStorageUpdateCheckTickInterval", Label: "Pal storage check interval", Description: "Tick interval used to check player Pal-storage updates.", Group: "performance", Type: "number", Min: number(0.1), Max: number(60), Step: number(0.1), Default: float64(1)},
		integer("MaxGuildsPerFrame", "Guilds processed per frame", "Maximum guilds processed during each server frame.", "performance", 10, 1, 10000),
		integer("BuildingNameDisplayCacheTTLSeconds", "Building-name cache lifetime", "Seconds that displayed building names remain cached.", "performance", 60, 0, 86400),
	}
}

func gamePresets() []GamePreset {
	return []GamePreset{
		{ID: "normal", Name: "Normal Night", Description: "Restore the gameplay baseline captured before the first preset.", Tone: "normal", Baseline: true, Changes: map[string]any{}},
		{ID: "fast-xp", Name: "Fast XP Night", Description: "Accelerate progression and make captures more forgiving.", Tone: "boost", Changes: map[string]any{
			"ExpRate": 3.0, "PalCaptureRate": 1.75,
		}},
		{ID: "breeding", Name: "Breeding Night", Description: "Short incubations, faster work and easier Pal upkeep.", Tone: "boost", Changes: map[string]any{
			"PalEggDefaultHatchingTime": 0.1, "WorkSpeedRate": 2.0, "PalStomachDecreaceRate": 0.5, "MonsterFarmActionSpeedRate": 2.0,
		}},
		{ID: "resources", Name: "Resource Farming", Description: "Increase gathering and drops while shortening resource respawns.", Tone: "boost", Changes: map[string]any{
			"CollectionDropRate": 3.0, "CollectionObjectRespawnSpeedRate": 0.5, "EnemyDropItemRate": 2.0, "WorkSpeedRate": 2.0, "MonsterFarmActionSpeedRate": 2.0,
		}},
		{ID: "boss-assist", Name: "Boss Assist", Description: "A strong advantage without completely deleting the fight.", Tone: "assist", Changes: map[string]any{
			"PlayerDamageRateAttack": 2.5, "PlayerDamageRateDefense": 0.4, "PlayerStaminaDecreaceRate": 0.5, "PlayerAutoHPRegeneRate": 3.0, "EquipmentDurabilityDamageRate": 0.25, "DeathPenalty": "None",
		}},
		{ID: "boss-delete", Name: "Boss Delete", Description: "Emergency cheat-like power for a fight the group wants finished.", Tone: "danger", Changes: map[string]any{
			"PlayerDamageRateAttack": 5.0, "PlayerDamageRateDefense": 0.1, "PlayerStaminaDecreaceRate": 0.1, "PlayerAutoHPRegeneRate": 5.0, "EquipmentDurabilityDamageRate": 0.1, "DeathPenalty": "None",
		}},
	}
}

func settingDefinitionMap() map[string]SettingDefinition {
	result := make(map[string]SettingDefinition)
	for _, definition := range gameSettingDefinitions() {
		result[definition.Key] = definition
	}
	return result
}

func officialSettingDefaults() map[string]any {
	result := make(map[string]any)
	for _, definition := range gameSettingDefinitions() {
		result[definition.Key] = definition.Default
	}
	return result
}

func normalizeSettingValue(definition SettingDefinition, value any) (any, error) {
	switch definition.Type {
	case "boolean":
		typed, ok := value.(bool)
		if !ok {
			return nil, errors.New("must be true or false")
		}
		return typed, nil
	case "number", "integer":
		var numeric float64
		switch typed := value.(type) {
		case float64:
			numeric = typed
		case float32:
			numeric = float64(typed)
		case int:
			numeric = float64(typed)
		case int64:
			numeric = float64(typed)
		case json.Number:
			parsed, err := typed.Float64()
			if err != nil {
				return nil, errors.New("must be a number")
			}
			numeric = parsed
		default:
			return nil, errors.New("must be a number")
		}
		if math.IsNaN(numeric) || math.IsInf(numeric, 0) {
			return nil, errors.New("must be a finite number")
		}
		if definition.Min != nil && numeric < *definition.Min {
			return nil, fmt.Errorf("must be at least %s", formatHumanNumber(*definition.Min))
		}
		if definition.Max != nil && numeric > *definition.Max {
			return nil, fmt.Errorf("must be at most %s", formatHumanNumber(*definition.Max))
		}
		if definition.Type == "integer" {
			if math.Trunc(numeric) != numeric {
				return nil, errors.New("must be a whole number")
			}
			return int64(numeric), nil
		}
		return numeric, nil
	case "select":
		typed, ok := value.(string)
		if !ok {
			return nil, errors.New("must be a supported option")
		}
		for _, option := range definition.Options {
			if typed == option.Value {
				return typed, nil
			}
		}
		return nil, errors.New("must be a supported option")
	case "text":
		typed, ok := value.(string)
		if !ok {
			return nil, errors.New("must be text")
		}
		typed = strings.TrimSpace(typed)
		if len(typed) > 200 {
			return nil, errors.New("must be 200 characters or fewer")
		}
		for _, character := range typed {
			if unicode.IsControl(character) {
				return nil, errors.New("contains unsupported control characters")
			}
		}
		return typed, nil
	case "list":
		var values []string
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				text, ok := item.(string)
				if !ok {
					return nil, errors.New("must contain only text values")
				}
				values = append(values, text)
			}
		case []string:
			values = append(values, typed...)
		default:
			return nil, errors.New("must be a list")
		}
		if len(values) > 100 {
			return nil, errors.New("must contain 100 entries or fewer")
		}
		clean := make([]string, 0, len(values))
		seen := make(map[string]bool)
		for _, item := range values {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			if len(item) > 100 {
				return nil, errors.New("list entries must be 100 characters or fewer")
			}
			for _, character := range item {
				if unicode.IsControl(character) || character == '"' || character == ',' || character == '(' || character == ')' {
					return nil, errors.New("list entries contain unsupported characters")
				}
			}
			seen[item] = true
			clean = append(clean, item)
		}
		return clean, nil
	default:
		return nil, errors.New("unsupported setting type")
	}
}

func validateSettingChanges(changes map[string]any) (map[string]any, error) {
	if len(changes) == 0 {
		return nil, errors.New("no setting changes were provided")
	}
	definitions := settingDefinitionMap()
	normalized := make(map[string]any, len(changes))
	for key, value := range changes {
		definition, ok := definitions[key]
		if !ok {
			return nil, fmt.Errorf("%s is not an editable Palworld setting", key)
		}
		clean, err := normalizeSettingValue(definition, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", definition.Label, err)
		}
		normalized[key] = clean
	}
	return normalized, nil
}

func encodeSettingValue(definition SettingDefinition, value any) (string, error) {
	normalized, err := normalizeSettingValue(definition, value)
	if err != nil {
		return "", err
	}
	switch definition.Type {
	case "boolean":
		if normalized.(bool) {
			return "True", nil
		}
		return "False", nil
	case "integer":
		return strconv.FormatInt(normalized.(int64), 10), nil
	case "number":
		return strconv.FormatFloat(normalized.(float64), 'f', 6, 64), nil
	case "select":
		text := normalized.(string)
		if definition.BareString {
			return text, nil
		}
		return strconv.Quote(text), nil
	case "text":
		return strconv.Quote(normalized.(string)), nil
	case "list":
		items := normalized.([]string)
		if len(items) == 0 {
			return "", nil
		}
		quoted := make([]string, len(items))
		for index, item := range items {
			quoted[index] = strconv.Quote(item)
		}
		return "(" + strings.Join(quoted, ",") + ")", nil
	default:
		return "", errors.New("unsupported setting type")
	}
}

func parseOptionSettings(content string) (string, []iniEntry, string, error) {
	marker := "OptionSettings=("
	start := strings.Index(content, marker)
	if start < 0 {
		return "", nil, "", errors.New("PalWorldSettings.ini does not contain OptionSettings")
	}
	bodyStart := start + len(marker)
	depth := 1
	quoted := false
	escaped := false
	end := -1
	for index := bodyStart; index < len(content); index++ {
		character := content[index]
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				quoted = false
			}
			continue
		}
		switch character {
		case '"':
			quoted = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = index
				index = len(content)
			}
		}
	}
	if end < 0 {
		return "", nil, "", errors.New("PalWorldSettings.ini has an unterminated OptionSettings value")
	}
	parts, err := splitTopLevel(content[bodyStart:end])
	if err != nil {
		return "", nil, "", err
	}
	entries := make([]iniEntry, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		separator := strings.Index(part, "=")
		if separator < 1 {
			return "", nil, "", fmt.Errorf("invalid OptionSettings entry %q", part)
		}
		key := strings.TrimSpace(part[:separator])
		value := strings.TrimSpace(part[separator+1:])
		entries = append(entries, iniEntry{Key: key, Value: value})
	}
	return content[:bodyStart], entries, content[end:], nil
}

func splitTopLevel(value string) ([]string, error) {
	parts := make([]string, 0)
	start := 0
	depth := 0
	quoted := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				quoted = false
			}
			continue
		}
		switch character {
		case '"':
			quoted = true
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("unexpected closing parenthesis in OptionSettings")
			}
		case ',':
			if depth == 0 {
				parts = append(parts, value[start:index])
				start = index + 1
			}
		}
	}
	if quoted || depth != 0 {
		return nil, errors.New("unterminated value in OptionSettings")
	}
	parts = append(parts, value[start:])
	return parts, nil
}

func applyINIChanges(content string, changes map[string]any) (string, error) {
	prefix, entries, suffix, err := parseOptionSettings(content)
	if err != nil {
		return "", err
	}
	definitions := settingDefinitionMap()
	indexByKey := make(map[string]int, len(entries))
	for index, entry := range entries {
		indexByKey[entry.Key] = index
	}
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		definition := definitions[key]
		encoded, err := encodeSettingValue(definition, changes[key])
		if err != nil {
			return "", fmt.Errorf("%s: %w", definition.Label, err)
		}
		if index, ok := indexByKey[key]; ok {
			entries[index].Value = encoded
		} else {
			indexByKey[key] = len(entries)
			entries = append(entries, iniEntry{Key: key, Value: encoded})
		}
	}
	values := make([]string, len(entries))
	for index, entry := range entries {
		values[index] = entry.Key + "=" + entry.Value
	}
	return prefix + strings.Join(values, ",") + suffix, nil
}

func parseINIValues(content string) (map[string]any, error) {
	_, entries, _, err := parseOptionSettings(content)
	if err != nil {
		return nil, err
	}
	definitions := settingDefinitionMap()
	values := officialSettingDefaults()
	for _, entry := range entries {
		definition, ok := definitions[entry.Key]
		if !ok {
			continue
		}
		parsed, err := decodeINIValue(definition, entry.Value)
		if err == nil {
			values[entry.Key] = parsed
		}
	}
	return values, nil
}

func decodeINIValue(definition SettingDefinition, value string) (any, error) {
	value = strings.TrimSpace(value)
	switch definition.Type {
	case "boolean":
		return strings.EqualFold(value, "true"), nil
	case "integer":
		return strconv.ParseInt(value, 10, 64)
	case "number":
		return strconv.ParseFloat(value, 64)
	case "select":
		return strings.Trim(value, `"`), nil
	case "text":
		if strings.HasPrefix(value, `"`) {
			return strconv.Unquote(value)
		}
		return value, nil
	case "list":
		value = strings.TrimSpace(value)
		if value == "" {
			return []string{}, nil
		}
		value = strings.TrimPrefix(value, "(")
		value = strings.TrimSuffix(value, ")")
		parts, err := splitTopLevel(value)
		if err != nil {
			return nil, err
		}
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, `"`) {
				unquoted, err := strconv.Unquote(part)
				if err != nil {
					return nil, err
				}
				part = unquoted
			}
			items = append(items, part)
		}
		return items, nil
	default:
		return nil, errors.New("unsupported setting type")
	}
}

func mergeEffectiveSettings(raw map[string]any) map[string]any {
	values := officialSettingDefaults()
	for _, definition := range gameSettingDefinitions() {
		apiKey := definition.APIKey
		if apiKey == "" {
			apiKey = definition.Key
		}
		if value, ok := raw[apiKey]; ok {
			if normalized, err := normalizeSettingValue(definition, value); err == nil {
				values[definition.Key] = normalized
			}
		}
	}
	return values
}

func (a *App) effectiveGameSettings(ctx context.Context) (map[string]any, string, error) {
	if a.cfg.Mock {
		a.settingsMu.Lock()
		defer a.settingsMu.Unlock()
		if a.mockSettings == nil {
			a.mockSettings = officialSettingDefaults()
		}
		return cloneSettings(a.mockSettings), "sample", nil
	}
	var raw map[string]any
	if err := a.pal.Get(ctx, "/v1/api/settings", &raw); err == nil {
		return mergeEffectiveSettings(raw), "live API", nil
	}
	if strings.TrimSpace(a.cfg.SettingsPath) == "" {
		return nil, "", errors.New("PALWORLD_SETTINGS_PATH is not configured")
	}
	data, err := os.ReadFile(a.cfg.SettingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("read Palworld settings: %w", err)
	}
	values, err := parseINIValues(string(data))
	if err != nil {
		return nil, "", err
	}
	return values, "configuration file", nil
}

func cloneSettings(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		if list, ok := value.([]string); ok {
			result[key] = append([]string(nil), list...)
		} else {
			result[key] = value
		}
	}
	return result
}

func (a *App) ensureSettingsBaseline(values map[string]any) error {
	if a.cfg.Mock {
		return nil
	}
	if strings.TrimSpace(a.cfg.SettingsStateDir) == "" {
		return errors.New("PALWORLD_SETTINGS_STATE_DIR is not configured")
	}
	if err := os.MkdirAll(a.cfg.SettingsStateDir, 0o750); err != nil {
		return fmt.Errorf("create settings state directory: %w", err)
	}
	path := filepath.Join(a.cfg.SettingsStateDir, "baseline.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	clean, err := validateSettingChanges(values)
	if err != nil {
		return fmt.Errorf("validate settings baseline: %w", err)
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(path, append(data, '\n'), 0o640)
}

func (a *App) readSettingsBaseline() (map[string]any, error) {
	if a.cfg.Mock {
		return officialSettingDefaults(), nil
	}
	path := filepath.Join(a.cfg.SettingsStateDir, "baseline.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read normal-night baseline: %w", err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("decode normal-night baseline: %w", err)
	}
	return validateSettingChanges(values)
}

func (a *App) settingsRollbackAvailable() bool {
	if a.cfg.Mock {
		return true
	}
	entries, err := filepath.Glob(filepath.Join(a.cfg.SettingsStateDir, "settings-*.ini"))
	return err == nil && len(entries) > 0
}

func (a *App) snapshotSettingsFile(content []byte) (string, error) {
	if err := os.MkdirAll(a.cfg.SettingsStateDir, 0o750); err != nil {
		return "", err
	}
	name := "settings-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".ini"
	path := filepath.Join(a.cfg.SettingsStateDir, name)
	if err := writeFileAtomically(path, content, 0o640); err != nil {
		return "", err
	}
	return path, nil
}

func latestSettingsSnapshot(directory string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(directory, "settings-*.ini"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", errors.New("there is no previous settings snapshot to restore")
	}
	sort.Strings(paths)
	return paths[len(paths)-1], nil
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".palctrl-settings-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	success = true
	return nil
}

func presetByID(id string) (GamePreset, bool) {
	for _, preset := range gamePresets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return GamePreset{}, false
}

func formatHumanNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
