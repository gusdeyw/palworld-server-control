package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (a *App) updatePalworld(ctx context.Context) (string, error) {
	if a.cfg.Mock {
		return a.docker.Update(ctx)
	}

	status, err := a.docker.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("check server before update: %w", err)
	}
	wasRunning := strings.EqualFold(strings.TrimSpace(status), "running")

	backupName := ""
	if wasRunning {
		_ = a.pal.Post(ctx, "/v1/api/announce", map[string]any{
			"message": "Server update starting. The world will restart when the update is ready.",
		})
		if err := a.pal.Post(ctx, "/v1/api/save", nil); err != nil {
			return "", fmt.Errorf("save world before update: %w", err)
		}
		timer := time.NewTimer(1200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}

	if a.cfg.SaveDir != "" && a.cfg.BackupDir != "" {
		backup, err := createBackup(a.cfg.SaveDir, a.cfg.BackupDir, false)
		if err != nil {
			return "", fmt.Errorf("create safety backup before update: %w", err)
		}
		backupName = backup.Name
	}

	if wasRunning && a.docker.updateRequiresStop() {
		if _, err := a.docker.Stop(ctx); err != nil {
			return "", fmt.Errorf("stop server before update: %w", err)
		}
	}

	result, err := a.docker.Update(ctx)
	if err != nil {
		if wasRunning {
			if recoveryErr := a.ensurePalworldRunning(ctx); recoveryErr != nil {
				return "", fmt.Errorf(
					"update failed: %v; old server recovery also failed: %w",
					err,
					recoveryErr,
				)
			}
		}
		return "", err
	}

	if err := a.ensurePalworldRunning(ctx); err != nil {
		return "", err
	}

	var info ServerInfo
	if err := a.pal.Get(ctx, "/v1/api/info", &info); err != nil {
		return "", fmt.Errorf("verify updated Palworld version: %w", err)
	}
	if version := strings.TrimSpace(info.Version); version != "" {
		result += ". Running Palworld " + version
	}
	if backupName != "" {
		result += ". Safety backup: " + backupName
	}
	return result, nil
}

func (a *App) ensurePalworldRunning(ctx context.Context) error {
	status, err := a.docker.Status(ctx)
	if err != nil {
		return fmt.Errorf("check server after update: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(status), "running") {
		if _, err := a.docker.Start(ctx); err != nil {
			return fmt.Errorf("start updated server: %w", err)
		}
	}
	if err := a.waitForPalworld(ctx, 120*time.Second); err != nil {
		return fmt.Errorf("updated server did not become healthy: %w", err)
	}
	return nil
}
