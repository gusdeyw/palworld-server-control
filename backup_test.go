package main

import (
	"archive/zip"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestDeleteBackupRemovesOnlyValidZIP(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "palworld-20260727-120000.zip"
	target := filepath.Join(backupDir, name)
	if err := os.WriteFile(target, []byte("backup"), 0o640); err != nil {
		t.Fatal(err)
	}
	deleted, err := deleteBackup(backupDir, name, false)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Name != name || deleted.Size != 6 {
		t.Fatalf("unexpected deleted backup: %#v", deleted)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected backup to be removed, got %v", err)
	}
}

func TestDeleteBackupRejectsUnsafeNamesAndNonZIPFiles(t *testing.T) {
	backupDir := t.TempDir()
	textFile := filepath.Join(backupDir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../outside.zip",
		"..\\outside.zip",
		"/absolute.zip",
		"notes.txt",
		"bad name.zip",
		"",
	} {
		if _, err := deleteBackup(backupDir, name, false); !errors.Is(err, errInvalidBackupName) {
			t.Fatalf("expected %q to be rejected, got %v", name, err)
		}
	}
	if content, err := os.ReadFile(textFile); err != nil || string(content) != "keep" {
		t.Fatalf("non-ZIP file changed: content=%q err=%v", content, err)
	}
}

func TestDeleteBackupHandlerRequiresMutationHeader(t *testing.T) {
	backupDir := t.TempDir()
	name := "palworld-20260727-130000.zip"
	target := filepath.Join(backupDir, name)
	if err := os.WriteFile(target, []byte("backup"), 0o640); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: Config{BackupDir: backupDir}}
	handler := app.requireMutation(http.HandlerFunc(app.handleDeleteBackup))

	deniedRequest := httptest.NewRequest(http.MethodDelete, "/api/backups/"+name, nil)
	deniedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("expected missing mutation header to return 403, got %d", deniedResponse.Code)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("backup changed after denied request: %v", err)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/backups/"+name, nil)
	deleteRequest.Header.Set("X-Pal-Control", "1")
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("expected successful deletion, got %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected backup to be deleted, got %v", err)
	}
}
