package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndListBackup(t *testing.T) {
	root := t.TempDir()
	saveDir := filepath.Join(root, "Saved")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Join(saveDir, "SaveGames"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saveDir, "SaveGames", "Level.sav"), []byte("test-save"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := createBackup(saveDir, backupDir, false)
	if err != nil {
		t.Fatalf("createBackup returned an error: %v", err)
	}
	if created.Size == 0 {
		t.Fatal("expected a non-empty backup")
	}

	archive, err := zip.OpenReader(filepath.Join(backupDir, created.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 2 {
		t.Fatalf("expected directory and save file in archive, got %d entries", len(archive.File))
	}

	backups, err := listBackups(backupDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Name != created.Name {
		t.Fatalf("unexpected backup list: %#v", backups)
	}
}
