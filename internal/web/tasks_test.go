package web

import (
	"errors"
	"math"
	"testing"
)

func TestDownloadTaskStoreTracksLifecycle(t *testing.T) {
	store := NewDownloadTaskStore()
	task := store.Create("esjzone", "1755960125")

	store.MarkRunning(task.ID, "esjzone", "1755960125", "example title", 12)
	store.MarkProgress(task.ID, 3, 12, "chapter 3")
	store.MarkExporting(task.ID, 12, 12)
	store.MarkCompleted(task.ID, "example title", []string{"a.epub"})

	snapshot, ok := store.Snapshot(task.ID)
	if !ok {
		t.Fatalf("expected task snapshot")
	}
	if snapshot.Status != "completed" {
		t.Fatalf("expected completed status, got %s", snapshot.Status)
	}
	if snapshot.Phase != "completed" {
		t.Fatalf("expected completed phase, got %s", snapshot.Phase)
	}
	if snapshot.Title != "example title" {
		t.Fatalf("expected title to be tracked, got %s", snapshot.Title)
	}
	if snapshot.CompletedChapters != 12 || snapshot.TotalChapters != 12 {
		t.Fatalf("expected chapter counters 12/12, got %d/%d", snapshot.CompletedChapters, snapshot.TotalChapters)
	}
	if len(snapshot.Exported) != 1 || snapshot.Exported[0] != "a.epub" {
		t.Fatalf("expected exported file, got %+v", snapshot.Exported)
	}
	if len(snapshot.Messages) < 4 {
		t.Fatalf("expected lifecycle messages, got %+v", snapshot.Messages)
	}
}

func TestDownloadTaskStoreTracksFailure(t *testing.T) {
	store := NewDownloadTaskStore()
	task := store.Create("esjzone", "1755960125")

	store.MarkFailed(task.ID, errors.New("download failed"))

	snapshot, ok := store.Snapshot(task.ID)
	if !ok {
		t.Fatalf("expected task snapshot")
	}
	if snapshot.Status != "failed" {
		t.Fatalf("expected failed status, got %s", snapshot.Status)
	}
	if snapshot.Error != "download failed" {
		t.Fatalf("expected failure message, got %s", snapshot.Error)
	}
	if snapshot.FinishedAt == nil {
		t.Fatalf("expected failure timestamp")
	}
}

func TestDownloadTaskSnapshotSanitizesNonFiniteSpeed(t *testing.T) {
	store := NewDownloadTaskStore()
	task := store.Create("esjzone", "1755960125")

	store.update(task.ID, func(item *DownloadTask) {
		item.Speed = math.Inf(1)
		item.ETA = "123秒"
	})

	snapshot, ok := store.Snapshot(task.ID)
	if !ok {
		t.Fatalf("expected task snapshot")
	}
	if snapshot.Speed != 0 {
		t.Fatalf("expected speed to be sanitized to 0, got %v", snapshot.Speed)
	}
	if snapshot.ETA != "" {
		t.Fatalf("expected eta to be cleared when speed is invalid, got %q", snapshot.ETA)
	}
}
