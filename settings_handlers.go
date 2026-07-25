package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func (a *App) handleGameSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	values, source, err := a.effectiveGameSettings(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	if err := a.ensureSettingsBaseline(values); err != nil {
		writeError(w, http.StatusInternalServerError, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{
		Groups:            gameSettingGroups(),
		Definitions:       gameSettingDefinitions(),
		Presets:           gamePresets(),
		Values:            values,
		Editable:          a.settingsEditable(),
		RollbackAvailable: a.settingsRollbackAvailable(),
		Source:            source,
		UpdatedAt:         time.Now().UTC(),
	})
}

func (a *App) handleApplyGameSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var request settingsApplyRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Preset = strings.TrimSpace(request.Preset)
	if request.Preset != "" && len(request.Changes) > 0 {
		writeError(w, http.StatusBadRequest, "choose either a preset or custom changes")
		return
	}
	if !a.settingsEditable() {
		writeError(w, http.StatusPreconditionFailed, "Palworld settings editing is not configured")
		return
	}

	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	current, _, err := a.effectiveGameSettingsUnlocked(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	if err := a.ensureSettingsBaseline(current); err != nil {
		writeError(w, http.StatusInternalServerError, friendlyError(err))
		return
	}

	label := "Custom settings"
	changes := request.Changes
	if request.Preset != "" {
		preset, ok := presetByID(request.Preset)
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown Game Night preset")
			return
		}
		label = preset.Name
		if preset.Baseline {
			changes, err = a.readSettingsBaseline()
			if err != nil {
				writeError(w, http.StatusInternalServerError, friendlyError(err))
				return
			}
		} else {
			changes = preset.Changes
		}
	}
	normalized, err := validateSettingChanges(changes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.cfg.Mock {
		if a.mockSettings == nil {
			a.mockSettings = officialSettingDefaults()
		}
		for key, value := range normalized {
			a.mockSettings[key] = value
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": label + " applied in sample mode",
		})
		return
	}

	backup, err := a.applySettingsAndRestart(ctx, normalized, label)
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": label + " applied; Palworld is ready",
		"backup":  backup,
	})
}

func (a *App) handleRollbackGameSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if !a.settingsEditable() {
		writeError(w, http.StatusPreconditionFailed, "Palworld settings editing is not configured")
		return
	}

	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()

	if a.cfg.Mock {
		a.mockSettings = officialSettingDefaults()
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Previous settings restored in sample mode",
		})
		return
	}

	target, err := latestSettingsSnapshot(a.cfg.SettingsStateDir)
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	targetData, err := os.ReadFile(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, friendlyError(err))
		return
	}
	if _, _, _, err := parseOptionSettings(string(targetData)); err != nil {
		writeError(w, http.StatusInternalServerError, "the previous settings snapshot is invalid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	backup, err := a.restoreSettingsAndRestart(ctx, targetData)
	if err != nil {
		writeError(w, http.StatusBadGateway, friendlyError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Previous settings restored; Palworld is ready",
		"backup":  backup,
	})
}

func (a *App) settingsEditable() bool {
	if a.cfg.Mock {
		return true
	}
	return strings.TrimSpace(a.cfg.SettingsPath) != "" &&
		strings.TrimSpace(a.cfg.SettingsStateDir) != ""
}

func (a *App) effectiveGameSettingsUnlocked(ctx context.Context) (map[string]any, string, error) {
	if a.cfg.Mock {
		if a.mockSettings == nil {
			a.mockSettings = officialSettingDefaults()
		}
		return cloneSettings(a.mockSettings), "sample", nil
	}
	var raw map[string]any
	if err := a.pal.Get(ctx, "/v1/api/settings", &raw); err == nil {
		return mergeEffectiveSettings(raw), "live API", nil
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

func (a *App) applySettingsAndRestart(
	ctx context.Context,
	changes map[string]any,
	label string,
) (Backup, error) {
	currentData, err := os.ReadFile(a.cfg.SettingsPath)
	if err != nil {
		return Backup{}, fmt.Errorf("read active Palworld settings: %w", err)
	}
	updated, err := applyINIChanges(string(currentData), changes)
	if err != nil {
		return Backup{}, err
	}
	if updated == string(currentData) {
		return Backup{}, errors.New("the selected settings are already active")
	}
	if _, err := a.snapshotSettingsFile(currentData); err != nil {
		return Backup{}, fmt.Errorf("snapshot current settings: %w", err)
	}

	backup, err := a.prepareSettingsRestart(ctx, label)
	if err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(a.cfg.SettingsPath)
	if err != nil {
		return Backup{}, err
	}
	if err := writeFileAtomically(a.cfg.SettingsPath, []byte(updated), info.Mode().Perm()); err != nil {
		return Backup{}, fmt.Errorf("write Palworld settings: %w", err)
	}
	if err := a.restartAfterSettingsChange(ctx); err != nil {
		restoreErr := writeFileAtomically(a.cfg.SettingsPath, currentData, info.Mode().Perm())
		if restoreErr == nil {
			_ = a.restartAfterSettingsChange(ctx)
			return Backup{}, fmt.Errorf("new settings failed; previous configuration restored: %w", err)
		}
		return Backup{}, fmt.Errorf("new settings failed and automatic restore failed: %v; restore error: %w", err, restoreErr)
	}
	return backup, nil
}

func (a *App) restoreSettingsAndRestart(ctx context.Context, targetData []byte) (Backup, error) {
	currentData, err := os.ReadFile(a.cfg.SettingsPath)
	if err != nil {
		return Backup{}, fmt.Errorf("read active Palworld settings: %w", err)
	}
	if string(currentData) == string(targetData) {
		return Backup{}, errors.New("the previous settings are already active")
	}
	if _, err := a.snapshotSettingsFile(currentData); err != nil {
		return Backup{}, fmt.Errorf("snapshot current settings: %w", err)
	}
	backup, err := a.prepareSettingsRestart(ctx, "Previous settings")
	if err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(a.cfg.SettingsPath)
	if err != nil {
		return Backup{}, err
	}
	if err := writeFileAtomically(a.cfg.SettingsPath, targetData, info.Mode().Perm()); err != nil {
		return Backup{}, fmt.Errorf("restore Palworld settings: %w", err)
	}
	if err := a.restartAfterSettingsChange(ctx); err != nil {
		restoreErr := writeFileAtomically(a.cfg.SettingsPath, currentData, info.Mode().Perm())
		if restoreErr == nil {
			_ = a.restartAfterSettingsChange(ctx)
			return Backup{}, fmt.Errorf("restored settings failed; newer configuration put back: %w", err)
		}
		return Backup{}, fmt.Errorf("restored settings failed and recovery failed: %v; recovery error: %w", err, restoreErr)
	}
	return backup, nil
}

func (a *App) prepareSettingsRestart(ctx context.Context, label string) (Backup, error) {
	if a.cfg.SaveDir == "" {
		return Backup{}, errors.New("PALWORLD_SAVE_DIR is required for safe settings changes")
	}
	if a.cfg.BackupDir == "" {
		return Backup{}, errors.New("PALWORLD_BACKUP_DIR is required for safe settings changes")
	}
	announcement := label + " will restart the server. Reconnect in about a minute."
	_ = a.pal.Post(ctx, "/v1/api/announce", map[string]any{"message": announcement})
	if err := a.pal.Post(ctx, "/v1/api/save", nil); err == nil {
		timer := time.NewTimer(1200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Backup{}, ctx.Err()
		case <-timer.C:
		}
	}
	backup, err := createBackup(a.cfg.SaveDir, a.cfg.BackupDir, false)
	if err != nil {
		return Backup{}, fmt.Errorf("create safety backup: %w", err)
	}
	return backup, nil
}

func (a *App) restartAfterSettingsChange(ctx context.Context) error {
	status, err := a.docker.Status(ctx)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(status), "running") {
		if _, err := a.docker.Restart(ctx); err != nil {
			return err
		}
	} else {
		if _, err := a.docker.Start(ctx); err != nil {
			return err
		}
	}
	return a.waitForPalworld(ctx, 90*time.Second)
}
