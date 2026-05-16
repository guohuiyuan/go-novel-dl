package config

import (
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoadDownloadTask(t *testing.T) {
	resetSiteCatalogForTest(t)

	now := time.Now().UTC().Truncate(time.Second)
	record := DownloadTaskRecord{
		ID:                "task-test-1",
		Site:              "esjzone",
		BookID:            "1755960125",
		Title:             "无题",
		Status:            "running",
		Phase:             "downloading",
		Target:            DownloadTaskTargetBrowser,
		Formats:           []string{"txt", "epub"},
		TotalChapters:     12,
		CompletedChapters: 3,
		Messages: []DownloadTaskMessageRecord{
			{At: now, Level: "info", Text: "任务已排队"},
		},
		StartTime: now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := SaveDownloadTask(record); err != nil {
		t.Fatalf("save task: %v", err)
	}

	loaded, ok, err := LoadDownloadTask(record.ID)
	if err != nil || !ok {
		t.Fatalf("load task: ok=%v err=%v", ok, err)
	}
	if loaded.Site != record.Site || loaded.BookID != record.BookID {
		t.Fatalf("identity mismatch: got %+v", loaded)
	}
	if loaded.Target != DownloadTaskTargetBrowser {
		t.Fatalf("expected target=browser, got %q", loaded.Target)
	}
	if len(loaded.Formats) != 2 || loaded.Formats[0] != "txt" || loaded.Formats[1] != "epub" {
		t.Fatalf("expected formats persisted, got %+v", loaded.Formats)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Text != "任务已排队" {
		t.Fatalf("expected one message persisted, got %+v", loaded.Messages)
	}
}

func TestListDownloadTasksOrderedByUpdatedAt(t *testing.T) {
	resetSiteCatalogForTest(t)

	now := time.Now().UTC()
	older := now.Add(-1 * time.Hour)

	if err := SaveDownloadTask(DownloadTaskRecord{
		ID: "older", Site: "esjzone", BookID: "1", Status: "completed", Phase: "completed",
		CreatedAt: older, UpdatedAt: older,
	}); err != nil {
		t.Fatalf("save older: %v", err)
	}
	if err := SaveDownloadTask(DownloadTaskRecord{
		ID: "newer", Site: "esjzone", BookID: "2", Status: "running", Phase: "downloading",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save newer: %v", err)
	}

	all, err := ListDownloadTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(all) != 2 || all[0].ID != "newer" || all[1].ID != "older" {
		t.Fatalf("expected tasks ordered by updated_at desc, got %+v", all)
	}
}

func TestMarkOrphanedDownloadTasksFailed(t *testing.T) {
	resetSiteCatalogForTest(t)

	now := time.Now().UTC()
	for _, status := range []string{"queued", "running", "completed"} {
		if err := SaveDownloadTask(DownloadTaskRecord{
			ID: "task-" + status, Site: "esjzone", BookID: status, Status: status, Phase: status,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("save %s: %v", status, err)
		}
	}

	count, err := MarkOrphanedDownloadTasksFailed("test restart")
	if err != nil {
		t.Fatalf("mark orphans failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 orphaned tasks transitioned, got %d", count)
	}

	for _, status := range []string{"queued", "running"} {
		loaded, ok, err := LoadDownloadTask("task-" + status)
		if err != nil || !ok {
			t.Fatalf("reload task-%s: ok=%v err=%v", status, ok, err)
		}
		if loaded.Status != "failed" || loaded.Phase != "failed" {
			t.Fatalf("expected task-%s to be failed, got status=%s phase=%s", status, loaded.Status, loaded.Phase)
		}
		if !strings.Contains(loaded.Error, "test restart") {
			t.Fatalf("expected failure error to mention reason, got %q", loaded.Error)
		}
		if loaded.FinishedAt == nil {
			t.Fatalf("expected finished_at to be set for task-%s", status)
		}
	}

	completed, ok, err := LoadDownloadTask("task-completed")
	if err != nil || !ok {
		t.Fatalf("reload task-completed: ok=%v err=%v", ok, err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected completed task to be left alone, got status=%s", completed.Status)
	}
}

func TestDeleteDownloadTask(t *testing.T) {
	resetSiteCatalogForTest(t)

	if err := SaveDownloadTask(DownloadTaskRecord{
		ID: "task-del", Site: "esjzone", BookID: "del", Status: "completed", Phase: "completed",
	}); err != nil {
		t.Fatalf("save task: %v", err)
	}
	if err := DeleteDownloadTask("task-del"); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	_, ok, err := LoadDownloadTask("task-del")
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if ok {
		t.Fatalf("expected task to be removed after delete")
	}
}
