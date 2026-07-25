package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Backup struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

func createBackup(saveDir, backupDir string, mock bool) (Backup, error) {
	if mock {
		return Backup{
			Name:      "palworld-sample-" + time.Now().Format("20060102-150405") + ".zip",
			Size:      184 * 1024 * 1024,
			CreatedAt: time.Now(),
		}, nil
	}
	if strings.TrimSpace(saveDir) == "" {
		return Backup{}, errors.New("PALWORLD_SAVE_DIR is not configured")
	}
	source, err := filepath.Abs(saveDir)
	if err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return Backup{}, fmt.Errorf("open save directory: %w", err)
	}
	if !info.IsDir() {
		return Backup{}, errors.New("PALWORLD_SAVE_DIR is not a directory")
	}
	destinationDir, err := filepath.Abs(backupDir)
	if err != nil {
		return Backup{}, err
	}
	if err := os.MkdirAll(destinationDir, 0o750); err != nil {
		return Backup{}, fmt.Errorf("create backup directory: %w", err)
	}
	name := "palworld-" + time.Now().Format("20060102-150405") + ".zip"
	target := filepath.Join(destinationDir, name)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return Backup{}, err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(target)
		}
	}()

	archive := zip.NewWriter(file)
	err = filepath.Walk(source, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		header, err := zip.FileInfoHeader(entry)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if entry.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = archive.Close()
		return Backup{}, fmt.Errorf("archive save directory: %w", err)
	}
	if err := archive.Close(); err != nil {
		return Backup{}, err
	}
	if err := file.Close(); err != nil {
		return Backup{}, err
	}
	success = true
	stat, err := os.Stat(target)
	if err != nil {
		return Backup{}, err
	}
	return Backup{Name: name, Size: stat.Size(), CreatedAt: stat.ModTime()}, nil
}

func listBackups(backupDir string, mock bool) ([]Backup, error) {
	if mock {
		now := time.Now()
		return []Backup{
			{Name: "palworld-sample-current.zip", Size: 184 * 1024 * 1024, CreatedAt: now.Add(-3 * time.Hour)},
			{Name: "palworld-sample-previous.zip", Size: 179 * 1024 * 1024, CreatedAt: now.Add(-27 * time.Hour)},
		}, nil
	}
	directory, err := filepath.Abs(backupDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	backups := make([]Backup, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, Backup{
			Name:      entry.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}
