package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/exporter"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/pipeline"
	"github.com/guohuiyuan/go-novel-dl/internal/progress"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/state"
	"github.com/guohuiyuan/go-novel-dl/internal/store"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

const AppName = "go-novel-dl"

type Runtime struct {
	Config   *config.Config
	Console  *ui.Console
	Registry *site.Registry
	Library  *store.Library
	Pipeline *pipeline.Runner
	Exporter *exporter.Service
	State    *state.Manager
	Progress progress.DownloadReporter
}

type DownloadResult struct {
	Book      *model.Book
	Stage     string
	Exported  []string
	Processed *model.Book
}

func NewRuntime(cfg *config.Config, console *ui.Console) *Runtime {
	return &Runtime{
		Config:   cfg,
		Console:  console,
		Registry: site.NewDefaultRegistry(),
		Library:  store.NewLibrary(cfg.General.RawDataDir),
		Pipeline: pipeline.New(),
		Exporter: exporter.New(),
		State:    state.NewManager(AppName),
		Progress: progress.NullReporter{},
	}
}

func LoadOrInitConfig(console *ui.Console, explicitPath string) (*config.Config, string, error) {
	cfg, path, err := config.Load(explicitPath)
	if err == nil {
		return cfg, path, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		target := explicitPath
		if target == "" {
			target = config.DefaultConfigFilename
		}

		absPath, absErr := filepath.Abs(target)
		if absErr != nil {
			absPath = target
		}
		console.Warnf("未找到配置文件，正在创建默认配置：%s", absPath)

		if err := config.WriteDefault(target, false); err != nil {
			return nil, "", err
		}
		console.Successf("已创建默认配置：%s", absPath)
		return config.Load(target)
	}

	return nil, "", err
}

func (r *Runtime) Download(ctx context.Context, siteKey string, books []model.BookRef, formats []string, skipExport bool) ([]DownloadResult, error) {
	resolved := r.Config.ResolveSiteConfig(siteKey)
	if len(books) == 0 {
		books = resolved.BookIDs
	}
	if len(books) == 0 {
		return nil, fmt.Errorf("渠道 %s 没有提供书籍 ID", siteKey)
	}

	client, err := r.Registry.Build(siteKey, resolved)
	if err != nil {
		return nil, err
	}

	if resolved.General.LoginRequired && resolved.Cookie == "" && resolved.Username == "" {
		r.Console.Warnf("渠道 %s 标记为需要登录，但当前未提供登录信息，将继续按占位适配流程执行", siteKey)
	}

	results := make([]DownloadResult, 0, len(books))
	for _, ref := range books {
		var existing *store.BookState
		existing, err = r.Library.LoadBookState(siteKey, ref.BookID, "raw")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return results, err
		}
		if err != nil && errors.Is(err, os.ErrNotExist) {
			existing = nil
		}

		book, err := client.DownloadPlan(ctx, ref)
		if err != nil {
			return results, err
		}
		if existing != nil {
			mergeExistingChapters(book, existing.Book)
		}
		book = textconv.NormalizeBookLocale(book, resolved.General.LocaleStyle)
		r.Progress.OnBookStart(siteKey, ref.BookID, book.Title, len(book.Chapters))
		done := 0
		for idx, chapter := range book.Chapters {
			if chapter.Downloaded && chapter.Content != "" {
				done++
				r.Progress.OnBookProgress(done, len(book.Chapters), chapter.Title)
				continue
			}
			loaded, err := client.FetchChapter(ctx, ref.BookID, chapter)
			if err != nil {
				r.Console.Warnf("跳过章节 %s: %v", chapter.Title, err)
				done++
				r.Progress.OnBookProgress(done, len(book.Chapters), chapter.Title)
				continue
			}
			book.Chapters[idx] = loaded
			done++
			r.Progress.OnBookProgress(done, len(book.Chapters), loaded.Title)
		}
		r.Progress.OnBookComplete(done, len(book.Chapters))
		book.Site = siteKey
		if book.DownloadedAt.IsZero() {
			book.DownloadedAt = time.Now().UTC()
		}
		book.UpdatedAt = time.Now().UTC()

		if err := r.Library.SaveBookStage(siteKey, "raw", book); err != nil {
			return results, err
		}

		processed, stage := r.Pipeline.Run(book, resolved.General.Processors)
		if processed == nil {
			processed = book
		}
		if stage == "" {
			stage = "raw"
		}
		if stage != "raw" {
			if err := r.Library.SaveBookStage(siteKey, stage, processed); err != nil {
				return results, err
			}
		}

		result := DownloadResult{Book: book, Processed: processed, Stage: stage}
		if !skipExport {
			exported, err := r.Exporter.Export(processed, siteKey, resolved.General.Output, resolved.General.OutputDir, formats)
			if err != nil {
				return results, err
			}
			result.Exported = exported
		}

		results = append(results, result)
	}

	return results, nil
}

func mergeExistingChapters(target *model.Book, existing *model.Book) {
	if target == nil || existing == nil {
		return
	}
	byID := make(map[string]model.Chapter, len(existing.Chapters))
	for _, chapter := range existing.Chapters {
		if (chapter.Downloaded || chapter.Content != "") && canReuseChapterContent(chapter.Content) {
			byID[chapter.ID] = chapter
		}
	}
	for idx, chapter := range target.Chapters {
		if cached, ok := byID[chapter.ID]; ok {
			target.Chapters[idx].Content = cached.Content
			target.Chapters[idx].Downloaded = true
			if target.Chapters[idx].Title == "" {
				target.Chapters[idx].Title = cached.Title
			}
		}
	}
}

func canReuseChapterContent(content string) bool {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "[\u63d2\u56fe]", "[\u63d2\u5716]", "[\u56fe\u7247]", "[\u5716\u7247]", "[??]":
			return false
		}
		if strings.HasPrefix(trimmed, "[??] ") {
			return false
		}
	}
	return strings.TrimSpace(content) != ""
}

func (r *Runtime) Search(ctx context.Context, sites []string, keyword string, overallLimit, perSiteLimit int) ([]model.SearchResult, error) {
	if len(sites) == 0 {
		sites = r.Registry.Keys()
	}

	results := make([]model.SearchResult, 0)
	for _, siteKey := range sites {
		resolved := r.Config.ResolveSiteConfig(siteKey)
		client, err := r.Registry.Build(siteKey, resolved)
		if err != nil {
			return results, err
		}

		limit := perSiteLimit
		if limit <= 0 {
			limit = 10
		}
		items, err := client.Search(ctx, keyword, limit)
		if err != nil {
			return results, err
		}
		results = append(results, items...)
		if overallLimit > 0 && len(results) >= overallLimit {
			return textconv.NormalizeSearchResultsLocale(results[:overallLimit], resolved.General.LocaleStyle), nil
		}
	}

	localeStyle := r.Config.General.LocaleStyle
	if len(sites) > 0 {
		localeStyle = r.Config.ResolveSiteConfig(sites[0]).General.LocaleStyle
	}
	return textconv.NormalizeSearchResultsLocale(results, localeStyle), nil
}

func (r *Runtime) Export(siteKey string, books []model.BookRef, stage string, formats []string) ([]string, error) {
	resolved := r.Config.ResolveSiteConfig(siteKey)
	if len(books) == 0 {
		books = resolved.BookIDs
	}
	if len(books) == 0 {
		return nil, fmt.Errorf("导出时没有提供书籍 ID")
	}

	created := make([]string, 0)
	for _, ref := range books {
		book, usedStage, err := r.Library.LoadBook(siteKey, ref.BookID, stage)
		if err != nil {
			return created, err
		}
		paths, err := r.Exporter.Export(book, siteKey, resolved.General.Output, resolved.General.OutputDir, formats)
		if err != nil {
			return created, err
		}
		created = append(created, paths...)
		r.Console.Infof("已从阶段 %s 导出 %s/%s", usedStage, siteKey, ref.BookID)
	}
	return created, nil
}

func (r *Runtime) ListStoredSites() ([]string, error) {
	return r.Library.ListSites()
}

func (r *Runtime) ListStoredBooks(siteKey string) ([]store.BookSummary, error) {
	return r.Library.ListBooks(siteKey)
}

func (r *Runtime) CleanLogs(dryRun bool) ([]string, error) {
	logDir := r.Config.General.Debug.LogDir
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	removed := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(logDir, entry.Name())
		removed = append(removed, path)
		if !dryRun {
			if err := os.Remove(path); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

func (r *Runtime) CleanCache(siteKey string) error {
	target := r.Config.General.CacheDir
	if siteKey != "" {
		target = filepath.Join(target, siteKey)
	}
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.RemoveAll(target)
}

func (r *Runtime) CleanAllBooks(siteKey string) error {
	return r.Library.RemoveAll(siteKey)
}

func (r *Runtime) CleanBooks(siteKey string, books []model.BookRef, stage string, removeChapters, removeMetadata, removeMedia, removeAll bool) error {
	for _, ref := range books {
		if err := r.Library.RemoveBook(siteKey, ref.BookID, stage, removeChapters, removeMetadata, removeMedia, removeAll); err != nil {
			return err
		}
	}
	return nil
}
