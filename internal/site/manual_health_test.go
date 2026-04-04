package site

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type manualHealthCase struct {
	siteKey    string
	bookURL    string
	chapterURL string
	chapterID  string
	timeout    time.Duration
}

const (
	manualHealthChapterLimit     = 20
	manualHealthSiteParallelism  = 4
	manualHealthFetchParallelism = 4
)

func TestManualDefaultRangeDownloadHealth(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_HEALTH") == "" {
		t.Skip("set GO_NOVEL_DL_HEALTH=1 to run manual site health checks")
	}

	cfg := loadManualHealthConfig(t)
	registry := NewDefaultRegistry()
	filter := parseManualHealthFilter(os.Getenv("GO_NOVEL_DL_HEALTH_SITES"))
	siteSlots := make(chan struct{}, manualHealthSiteParallelism)

	testCases := []manualHealthCase{
		{siteKey: "esjzone", bookURL: "https://www.esjzone.cc/detail/1660702902.html", chapterURL: "https://www.esjzone.cc/forum/1660702902/294593.html", timeout: 3 * time.Minute},
		{siteKey: "westnovel", bookURL: "https://www.westnovel.com/ksl/sq/", chapterURL: "https://www.westnovel.com/ksl/sq/140072.html", timeout: 2 * time.Minute},
		{siteKey: "yibige", bookURL: "https://www.yibige.org/6238/", chapterURL: "https://www.yibige.org/6238/1.html", timeout: 2 * time.Minute},
		{siteKey: "yodu", bookURL: "https://www.yodu.org/book/18862/", chapterURL: "https://www.yodu.org/book/18862/4662939.html", timeout: 2 * time.Minute},
		{siteKey: "linovelib", bookURL: "https://www.linovelib.com/novel/1234.html", chapterURL: "https://www.linovelib.com/novel/1234/47800.html", timeout: 3 * time.Minute},
		{siteKey: "n23qb", bookURL: "https://www.23qb.com/book/12282/", chapterURL: "https://www.23qb.com/book/12282/7908999.html", timeout: 5 * time.Minute},
		{siteKey: "biquge345", bookURL: "https://www.biquge345.com/book/151120/", chapterURL: "https://www.biquge345.com/chapter/151120/79336811.html", timeout: 3 * time.Minute},
		{siteKey: "biquge5", bookURL: "https://www.biquge5.com/9_9194/", chapterURL: "https://www.biquge5.com/9_9194/737908.html", timeout: 2 * time.Minute},
		{siteKey: "fsshu", bookURL: "https://www.fsshu.com/biquge/0_139/", chapterURL: "https://www.fsshu.com/biquge/0_139/c40381.html", timeout: 2 * time.Minute},
		{siteKey: "n69shuba", bookURL: "https://www.69shuba.com/book/88724.htm", chapterURL: "https://www.69shuba.com/txt/88724/39943182", timeout: 2 * time.Minute},
		{siteKey: "piaotia", bookURL: "https://www.piaotia.com/bookinfo/1/1705.html", chapterURL: "https://www.piaotia.com/html/1/1705/762992.html", timeout: 7 * time.Minute},
		{siteKey: "ixdzs8", bookURL: "https://ixdzs8.com/read/38804/", chapterURL: "https://ixdzs8.com/read/38804/p1.html", timeout: 6 * time.Minute},
		{siteKey: "n8novel", bookURL: "https://www.8novel.com/novelbooks/109806/", chapterURL: "https://article.8novel.com/read/109806/?1769597", timeout: 6 * time.Minute},
		{siteKey: "novalpie", bookURL: "https://novalpie.jp/novel/2393?sid=main5", chapterURL: "https://novalpie.jp/viewer/51118", timeout: 3 * time.Minute},
		{siteKey: "ruochu", bookURL: "https://www.ruochu.com/book/158713", chapterURL: "https://www.ruochu.com/book/158713/13869103", timeout: 2 * time.Minute},
		{siteKey: "n17k", bookURL: "https://www.17k.com/book/3631088.html", chapterURL: "https://www.17k.com/chapter/3631088/49406153.html", timeout: 2 * time.Minute},
		{siteKey: "hongxiuzhao", bookURL: "https://hongxiuzhao.net/ZG6rmWO.html", chapterURL: "https://hongxiuzhao.net/aBKBVz6a.html", timeout: 2 * time.Minute},
		{siteKey: "fanqienovel", bookURL: "https://fanqienovel.com/page/7276384138653862966", chapterURL: "https://fanqienovel.com/reader/7276663560427471412", chapterID: "7276663560427471412", timeout: 3 * time.Minute},
		{siteKey: "faloo", bookURL: "https://b.faloo.com/1482723.html", chapterURL: "https://b.faloo.com/1482723_1.html", timeout: 5 * time.Minute},
		{siteKey: "wenku8", bookURL: "https://www.wenku8.net/book/2835.htm", chapterURL: "https://www.wenku8.net/novel/2/2835/113354.htm", timeout: 3 * time.Minute},
		{siteKey: "sfacg", bookURL: "https://m.sfacg.com/b/456123/", chapterURL: "https://m.sfacg.com/c/5417665/", timeout: 3 * time.Minute},
		{siteKey: "ciyuanji", bookURL: "https://www.ciyuanji.com/b_d_12030.html", chapterURL: "https://www.ciyuanji.com/chapter/12030_3046684.html", timeout: 3 * time.Minute},
		{siteKey: "qbtr", bookURL: "https://www.qbtr.cc/tongren/8978.html", chapterURL: "https://www.qbtr.cc/tongren/8978/1.html", timeout: 2 * time.Minute},
		{siteKey: "ciweimao", bookURL: "https://www.ciweimao.com/book/100011781", chapterURL: "https://www.ciweimao.com/chapter/100257072", timeout: 3 * time.Minute},
	}

	for _, tc := range testCases {
		tc := tc
		if len(filter) > 0 {
			if _, ok := filter[tc.siteKey]; !ok {
				continue
			}
		}

		t.Run(tc.siteKey, func(t *testing.T) {
			t.Parallel()
			siteSlots <- struct{}{}
			defer func() { <-siteSlots }()

			resolvedCfg := cfg.ResolveSiteConfig(tc.siteKey)
			if resolvedCfg.General.Timeout < 20 {
				resolvedCfg.General.Timeout = 20
			}
			if resolvedCfg.General.RequestInterval > 0.2 {
				resolvedCfg.General.RequestInterval = 0.2
			}
			if resolvedCfg.General.RetryTimes < 1 {
				resolvedCfg.General.RetryTimes = 1
			}

			if resolvedCfg.General.LoginRequired && strings.TrimSpace(resolvedCfg.Cookie) == "" && strings.TrimSpace(resolvedCfg.Username) == "" {
				t.Skipf("site %s requires login but no credentials are configured", tc.siteKey)
			}

			client, err := registry.Build(tc.siteKey, resolvedCfg)
			if err != nil {
				t.Fatalf("build site %s: %v", tc.siteKey, err)
			}

			bookRef, expectedChapterID := resolveManualHealthRefs(t, client, tc)

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			book, err := client.DownloadPlan(ctx, bookRef)
			if err != nil {
				t.Fatalf("download plan %s/%s: %v", tc.siteKey, bookRef.BookID, err)
			}
			if book == nil {
				t.Fatalf("download plan %s/%s returned nil book", tc.siteKey, bookRef.BookID)
			}
			if book.ID != bookRef.BookID {
				t.Fatalf("unexpected book id: got %s want %s", book.ID, bookRef.BookID)
			}
			if strings.TrimSpace(book.Title) == "" {
				t.Fatalf("book %s/%s has empty title", tc.siteKey, bookRef.BookID)
			}
			if len(book.Chapters) == 0 {
				t.Fatalf("book %s/%s returned no chapters", tc.siteKey, bookRef.BookID)
			}

			chapters := book.Chapters
			truncated := false
			if len(chapters) > manualHealthChapterLimit {
				chapters = append([]model.Chapter(nil), chapters[:manualHealthChapterLimit]...)
				truncated = true
				t.Logf("章节数量 %d 超过上限 %d，已中断剩余章节下载", len(book.Chapters), manualHealthChapterLimit)
			}

			loaded, err := fetchManualHealthChapters(ctx, client, bookRef.BookID, chapters)
			if err != nil {
				t.Fatalf("fetch chapters %s/%s: %v", tc.siteKey, bookRef.BookID, err)
			}
			book.Chapters = loaded

			foundExpectedChapter := false
			for idx, chapter := range book.Chapters {
				if chapter.ID == expectedChapterID {
					foundExpectedChapter = true
				}
				if !chapter.Downloaded {
					t.Fatalf("chapter %d is not marked downloaded: %+v", idx, chapter)
				}
				if strings.TrimSpace(chapter.Title) == "" {
					t.Fatalf("chapter %d has empty title", idx)
				}
				if strings.TrimSpace(chapter.Content) == "" {
					t.Fatalf("chapter %d has empty content", idx)
				}
			}
			if !foundExpectedChapter {
				if truncated {
					t.Logf("expected chapter %s not in first %d chapters after truncation", expectedChapterID, manualHealthChapterLimit)
				} else {
					t.Fatalf("catalog for %s/%s does not contain expected chapter %s", tc.siteKey, bookRef.BookID, expectedChapterID)
				}
			}

			t.Logf("downloaded %d chapters for %s/%s", len(book.Chapters), tc.siteKey, bookRef.BookID)
		})
	}
}

func fetchManualHealthChapters(ctx context.Context, client Site, bookID string, chapters []model.Chapter) ([]model.Chapter, error) {
	if len(chapters) == 0 {
		return nil, nil
	}
	loaded := make([]model.Chapter, len(chapters))

	jobs := make(chan int)
	errCh := make(chan error, 1)
	workers := manualHealthFetchParallelism
	if workers > len(chapters) {
		workers = len(chapters)
	}

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					return
				}
				chapter, err := client.FetchChapter(ctx, bookID, chapters[idx])
				if err != nil {
					select {
					case errCh <- fmt.Errorf("chapter %s: %w", chapters[idx].ID, err):
					default:
					}
					return
				}
				loaded[idx] = chapter
			}
		}()
	}

	for idx := range chapters {
		select {
		case err := <-errCh:
			close(jobs)
			wg.Wait()
			return nil, err
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	return loaded, nil
}

func resolveManualHealthRefs(t *testing.T, client Site, tc manualHealthCase) (model.BookRef, string) {
	t.Helper()

	resolvedBook, ok := client.ResolveURL(tc.bookURL)
	if !ok || resolvedBook == nil || strings.TrimSpace(resolvedBook.BookID) == "" {
		t.Fatalf("resolve book url for %s failed: %s", tc.siteKey, tc.bookURL)
	}

	resolvedChapter, ok := client.ResolveURL(tc.chapterURL)
	if !ok || resolvedChapter == nil || strings.TrimSpace(resolvedChapter.ChapterID) == "" {
		t.Fatalf("resolve chapter url for %s failed: %s", tc.siteKey, tc.chapterURL)
	}

	if resolvedChapter.BookID != "" && resolvedChapter.BookID != resolvedBook.BookID {
		t.Fatalf("resolved book mismatch for %s: book=%s chapter=%s", tc.siteKey, resolvedBook.BookID, resolvedChapter.BookID)
	}

	expectedChapterID := strings.TrimSpace(tc.chapterID)
	if expectedChapterID == "" {
		expectedChapterID = resolvedChapter.ChapterID
	}

	return model.BookRef{BookID: resolvedBook.BookID}, expectedChapterID
}

func loadManualHealthConfig(t *testing.T) *config.Config {
	t.Helper()

	root := manualHealthProjectRoot(t)
	cfgPath := filepath.Join(root, "data", "settings.toml")
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("load config %s: %v", cfgPath, err)
		}
		defaults := config.DefaultConfig()
		cfg = &defaults
	}

	cfg.General.RawDataDir = manualHealthAbsPath(root, cfg.General.RawDataDir)
	cfg.General.OutputDir = manualHealthAbsPath(root, cfg.General.OutputDir)
	cfg.General.CacheDir = manualHealthAbsPath(root, cfg.General.CacheDir)
	cfg.General.Debug.LogDir = manualHealthAbsPath(root, cfg.General.Debug.LogDir)
	cfg.Plugins.LocalPluginsPath = manualHealthAbsPath(root, cfg.Plugins.LocalPluginsPath)
	return cfg
}

func manualHealthProjectRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func manualHealthAbsPath(root, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func parseManualHealthFilter(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	filter := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		filter[item] = struct{}{}
	}
	return filter
}
