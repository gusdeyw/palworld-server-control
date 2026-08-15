package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
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

func TestBackupDownloadIsAuthenticatedAndSupportsRanges(t *testing.T) {
	backupDir := t.TempDir()
	name := "palworld-20260815-120000.zip"
	content := []byte("PK sample backup payload")
	if err := os.WriteFile(filepath.Join(backupDir, name), content, 0o640); err != nil {
		t.Fatal(err)
	}
	app := &App{
		cfg:     Config{BackupDir: backupDir},
		session: "test-session",
	}
	handler := app.requireAuth(http.HandlerFunc(app.handleBackupEntry))

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/backups/"+name+"/download", nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated download to return 401, got %d", unauthorizedResponse.Code)
	}

	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/backups/"+name+"/download", nil)
	downloadRequest.AddCookie(&http.Cookie{Name: "palctrl_session", Value: "test-session"})
	downloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(downloadResponse, downloadRequest)
	if downloadResponse.Code != http.StatusOK {
		t.Fatalf("download returned %d: %s", downloadResponse.Code, downloadResponse.Body.String())
	}
	if got := downloadResponse.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("unexpected content type %q", got)
	}
	if got := downloadResponse.Header().Get("Content-Disposition"); got != `attachment; filename="`+name+`"` {
		t.Fatalf("unexpected content disposition %q", got)
	}
	if got := downloadResponse.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("unexpected cache control %q", got)
	}
	if got := downloadResponse.Body.Bytes(); string(got) != string(content) {
		t.Fatalf("unexpected download body %q", got)
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, "/api/backups/"+name+"/download", nil)
	rangeRequest.AddCookie(&http.Cookie{Name: "palctrl_session", Value: "test-session"})
	rangeRequest.Header.Set("Range", "bytes=3-8")
	rangeResponse := httptest.NewRecorder()
	handler.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent {
		t.Fatalf("range download returned %d: %s", rangeResponse.Code, rangeResponse.Body.String())
	}
	if got := rangeResponse.Body.String(); got != string(content[3:9]) {
		t.Fatalf("unexpected range body %q", got)
	}
}

func TestBackupEntryKeepsDeleteBehindMutationHeader(t *testing.T) {
	backupDir := t.TempDir()
	name := "palworld-20260815-121000.zip"
	target := filepath.Join(backupDir, name)
	if err := os.WriteFile(target, []byte("backup"), 0o640); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: Config{BackupDir: backupDir}}

	request := httptest.NewRequest(http.MethodDelete, "/api/backups/"+name, nil)
	response := httptest.NewRecorder()
	app.handleBackupEntry(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected deletion without mutation header to return 403, got %d", response.Code)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("backup changed after denied deletion: %v", err)
	}
}

func TestBackupDownloadRejectsUnsafeAndMissingNames(t *testing.T) {
	app := &App{cfg: Config{BackupDir: t.TempDir()}}

	unsafeRequest := httptest.NewRequest(http.MethodGet, "/api/backups/bad%20name.zip/download", nil)
	unsafeResponse := httptest.NewRecorder()
	app.handleBackupEntry(unsafeResponse, unsafeRequest)
	if unsafeResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected unsafe name to return 400, got %d", unsafeResponse.Code)
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/backups/missing.zip/download", nil)
	missingResponse := httptest.NewRecorder()
	app.handleBackupEntry(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected missing backup to return 404, got %d", missingResponse.Code)
	}
}

func TestMockBackupDownloadProducesValidZIP(t *testing.T) {
	app := &App{cfg: Config{Mock: true}}
	request := httptest.NewRequest(http.MethodGet, "/api/backups/palworld-sample-current.zip/download", nil)
	response := httptest.NewRecorder()
	app.handleBackupEntry(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("mock download returned %d: %s", response.Code, response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatalf("mock download is not a valid ZIP: %v", err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != "README.txt" {
		t.Fatalf("unexpected mock archive entries: %#v", archive.File)
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || len(body) == 0 {
		t.Fatalf("unexpected mock archive body %q: %v", body, err)
	}
}
