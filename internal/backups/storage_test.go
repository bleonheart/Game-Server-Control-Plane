package backups

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorageCreateRestore(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	backupRoot := filepath.Join(root, "backups")
	destination := filepath.Join(root, "restore")
	if err := os.MkdirAll(filepath.Join(source, "garrysmod", "data"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "garrysmod", "data", "state.txt"), []byte("persistent-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "srcds_run"), []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	storage := NewLocalStorage(backupRoot)
	location, size, sha, err := storage.Create(context.Background(), "srv_test", "bak_test", source)
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 || len(sha) != 64 {
		t.Fatalf("invalid metadata size=%d sha=%q", size, sha)
	}
	if err := storage.Validate(context.Background(), location, sha); err != nil {
		t.Fatal(err)
	}
	if err := storage.Restore(context.Background(), location, destination); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "garrysmod", "data", "state.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "persistent-data" {
		t.Fatalf("unexpected restored data: %q", body)
	}
	stat, err := os.Stat(filepath.Join(destination, "srcds_run"))
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode()&0o100 == 0 {
		t.Fatal("restored executable lost owner execute permission")
	}
	file, err := os.OpenFile(location, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("tampered")); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := storage.Validate(context.Background(), location, sha); err != ErrChecksumMismatch {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}
