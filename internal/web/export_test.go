package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/store"
)

func TestRunExportTaskUsesCompleteLocalCacheWithoutDownload(t *testing.T) {
	service := newTestService()
	cfg := service.Config
	rawDir := filepath.Join(t.TempDir(), "raw")
	outDir := filepath.Join(t.TempDir(), "downloads")
	cfg.General.RawDataDir = rawDir
	cfg.General.OutputDir = outDir
	cfg.General.Output.Formats = []string{"txt"}
	cfg.General.Output.AppendTimestamp = false

	runtime := service.Runtime
	runtime.Library = store.NewLibrary(rawDir)
	book := &model.Book{
		Site:   "esjzone",
		ID:     "001",
		Title:  "本地书",
		Author: "作者",
		Chapters: []model.Chapter{
			{ID: "c1", Title: "第一章", Content: "正文"},
		},
	}
	if err := runtime.Library.SaveBookStage("esjzone", "raw", book); err != nil {
		t.Fatalf("save local book: %v", err)
	}

	var calls int32
	runtime.Registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return fakeWebSite{
			key:               "esjzone",
			capabilities:      site.Capabilities{Download: true},
			downloadPlanCalls: &calls,
		}
	})

	task := service.Tasks.Create("esjzone", "001", DownloadTaskOptions{
		Target:  DownloadTaskTargetExport,
		Formats: []string{"txt"},
	})
	result, err := service.runExportTask(task.ID, runtime, downloadRequest{
		Site:    "esjzone",
		BookID:  "001",
		Formats: []string{"txt"},
		Target:  DownloadTaskTargetExport,
	})
	if err != nil {
		t.Fatalf("run export task: %v", err)
	}
	if len(result.Exported) != 1 {
		t.Fatalf("expected 1 exported file, got %d", len(result.Exported))
	}
	if _, err := os.Stat(result.Exported[0]); err != nil {
		t.Fatalf("exported file missing: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no site download plan calls when local cache is complete, got %d", got)
	}
}

func TestRunExportTaskFallsBackToLocalCacheWhenDownloadFails(t *testing.T) {
	service := newTestService()
	cfg := service.Config
	rawDir := filepath.Join(t.TempDir(), "raw")
	outDir := filepath.Join(t.TempDir(), "downloads")
	cfg.General.RawDataDir = rawDir
	cfg.General.OutputDir = outDir
	cfg.General.Output.Formats = []string{"txt"}
	cfg.General.Output.AppendTimestamp = false

	runtime := service.Runtime
	runtime.Library = store.NewLibrary(rawDir)
	book := &model.Book{
		Site:   "esjzone",
		ID:     "001",
		Title:  "缓存书",
		Author: "作者",
		Chapters: []model.Chapter{
			{ID: "c1", Title: "第一章", Content: "已缓存正文"},
			{ID: "c2", Title: "第二章", Content: ""},
		},
	}
	if err := runtime.Library.SaveBookStage("esjzone", "raw", book); err != nil {
		t.Fatalf("save local book: %v", err)
	}

	runtime.Registry.Register("esjzone", func(cfg config.ResolvedSiteConfig) site.Site {
		return failingWebSite{fakeWebSite: fakeWebSite{
			key:          "esjzone",
			capabilities: site.Capabilities{Download: true},
		}}
	})

	task := service.Tasks.Create("esjzone", "001", DownloadTaskOptions{
		Target:  DownloadTaskTargetExport,
		Formats: []string{"txt"},
	})
	result, err := service.runExportTask(task.ID, runtime, downloadRequest{
		Site:    "esjzone",
		BookID:  "001",
		Formats: []string{"txt"},
		Target:  DownloadTaskTargetExport,
	})
	if err != nil {
		t.Fatalf("expected fallback export to succeed, got %v", err)
	}
	if len(result.Exported) != 1 {
		t.Fatalf("expected 1 exported file from cache fallback, got %d", len(result.Exported))
	}
	if _, err := os.Stat(result.Exported[0]); err != nil {
		t.Fatalf("fallback exported file missing: %v", err)
	}
}

func TestDownloadFileUsesBasename(t *testing.T) {
	service := newTestService()
	router := newRouter(service)

	dir := t.TempDir()
	target := filepath.Join(dir, "safe-download.txt")
	if err := os.WriteFile(target, []byte("content"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/download-file?path="+url.QueryEscape(target), nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if disposition := resp.Header().Get("Content-Disposition"); !strings.Contains(disposition, "safe-download.txt") {
		t.Fatalf("expected attachment filename to use basename, got %q", disposition)
	}
}

func TestTaskFileChipsUseExplicitDownloadBasename(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	if strings.Contains(script, `link.download = "";`) {
		t.Fatalf("expected download link to use the exported file basename")
	}
	if !strings.Contains(script, `link.download = basename;`) {
		t.Fatalf("expected app.js to set link.download from basename")
	}
}

type failingWebSite struct {
	fakeWebSite
}

func (s failingWebSite) DownloadPlan(context.Context, model.BookRef) (*model.Book, error) {
	return nil, errors.New("network unavailable")
}

func (s failingWebSite) FetchChapter(context.Context, string, model.Chapter) (model.Chapter, error) {
	return model.Chapter{}, errors.New("network unavailable")
}

func (s failingWebSite) Download(context.Context, model.BookRef) (*model.Book, error) {
	return nil, errors.New("network unavailable")
}
