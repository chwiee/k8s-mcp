package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKBStoreReloadsUpdatedDocuments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pods.md")
	if err := os.WriteFile(path, []byte("CrashLoopBackOff guidance"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	store, err := NewKBStore(dir)
	if err != nil {
		t.Fatalf("NewKBStore() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store.Start(ctx, 10*time.Millisecond, slog.Default())

	if err := os.WriteFile(path, []byte("ImagePullBackOff guidance"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		matches := store.Search([]string{"imagepullbackoff"})
		if len(matches) > 0 && strings.Contains(matches[0], "ImagePullBackOff") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("knowledge base did not reload updated content")
}
