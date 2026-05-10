package site

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type candidateHealthCase struct {
	siteKey    string
	keyword    string
	bookURL    string
	chapterURL string
	chapterID  string
	timeout    time.Duration
}

func TestManualCandidateSourceHealth(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_CANDIDATE_HEALTH") == "" {
		t.Skip("set GO_NOVEL_DL_CANDIDATE_HEALTH=1 to run candidate source health checks")
	}

	cfg := loadManualHealthConfig(t)
	registry := NewDefaultRegistry()
	registerCandidateHealthSites(registry)
	filter := parseManualHealthFilter(os.Getenv("GO_NOVEL_DL_HEALTH_SITES"))

	testCases := []candidateHealthCase{
		{siteKey: "esjzone", keyword: "魔女", bookURL: "https://www.esjzone.cc/detail/1660702902.html", chapterURL: "https://www.esjzone.cc/forum/1660702902/294593.html", timeout: 3 * time.Minute},
		{siteKey: "biquge345", keyword: "斗罗", bookURL: "https://www.biquge345.com/book/151120/", chapterURL: "https://www.biquge345.com/chapter/151120/79336811.html", timeout: 3 * time.Minute},
		{siteKey: "n69shuba", keyword: "斗罗", bookURL: "https://www.69shuba.com/book/88724.htm", chapterURL: "https://www.69shuba.com/txt/88724/39943182", timeout: 2 * time.Minute},
		{siteKey: "novalpie", keyword: "hunter", bookURL: "https://novalpie.cc/novel/1059", chapterURL: "https://novalpie.cc/viewer/245640", timeout: 3 * time.Minute},
		{siteKey: "n17k", keyword: "诡秘", bookURL: "https://www.17k.com/book/3631088.html", chapterURL: "https://www.17k.com/chapter/3631088/49406153.html", timeout: 2 * time.Minute},
		{siteKey: "hongxiuzhao", keyword: "斗罗", bookURL: "https://hongxiuzhao.net/ZG6rmWO.html", chapterURL: "https://hongxiuzhao.net/aBKBVz6a.html", chapterID: "aBKBVz6a", timeout: 2 * time.Minute},
		{siteKey: "faloo", keyword: "原神", bookURL: "https://b.faloo.com/1482723.html", chapterURL: "https://b.faloo.com/1482723_1.html", timeout: 5 * time.Minute},
		{siteKey: "tongrenshe", keyword: "斗罗", bookURL: "https://tongrenshe.cc/tongren/8899.html", chapterURL: "https://tongrenshe.cc/tongren/8899/1.html", timeout: 2 * time.Minute},
		{siteKey: "westnovel", keyword: "斗罗", bookURL: "https://www.westnovel.com/ksl/sq/", chapterURL: "https://www.westnovel.com/ksl/sq/140072.html", timeout: 90 * time.Second},
		{siteKey: "biquge5", keyword: "斗破", bookURL: "https://www.biquge5.com/9_9194/", chapterURL: "https://www.biquge5.com/9_9194/457101.html", timeout: 2 * time.Minute},
		{siteKey: "piaotia", keyword: "斗破", bookURL: "https://www.piaotia.com/bookinfo/1/1705.html", chapterURL: "https://www.piaotia.com/html/1/1705/762992.html", timeout: 2 * time.Minute},
		{siteKey: "qbtr", keyword: "斗罗", bookURL: "https://www.qbtr.cc/tongren/8978.html", chapterURL: "https://www.qbtr.cc/tongren/8978/1.html", timeout: 90 * time.Second},
	}

	for _, tc := range testCases {
		tc := tc
		if len(filter) > 0 {
			if _, ok := filter[tc.siteKey]; !ok {
				continue
			}
		}

		t.Run(tc.siteKey, func(t *testing.T) {
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

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			start := time.Now()
			results, searchErr := client.Search(ctx, tc.keyword, 5)
			searchElapsed := time.Since(start)
			if searchErr != nil {
				t.Fatalf("search failed after %s: %v", searchElapsed.Round(time.Millisecond), searchErr)
			}
			if len(results) == 0 {
				t.Fatalf("search returned no results after %s", searchElapsed.Round(time.Millisecond))
			}

			bookRef, expectedChapterID := resolveCandidateHealthRefs(t, client, tc)
			start = time.Now()
			book, err := client.DownloadPlan(ctx, bookRef)
			detailElapsed := time.Since(start)
			if err != nil {
				t.Fatalf("download plan failed after %s: %v", detailElapsed.Round(time.Millisecond), err)
			}
			if err := validateCandidateBook(tc.siteKey, bookRef, book); err != nil {
				t.Fatalf("invalid book after %s: %v", detailElapsed.Round(time.Millisecond), err)
			}

			chapter := firstCandidateChapter(book.Chapters, expectedChapterID)
			start = time.Now()
			loaded, err := client.FetchChapter(ctx, bookRef.BookID, chapter)
			chapterElapsed := time.Since(start)
			if err != nil {
				t.Fatalf("fetch chapter failed after %s: %v", chapterElapsed.Round(time.Millisecond), err)
			}
			if !loaded.Downloaded || strings.TrimSpace(loaded.Title) == "" || strings.TrimSpace(loaded.Content) == "" {
				t.Fatalf("invalid chapter after %s: downloaded=%v title=%q content_len=%d", chapterElapsed.Round(time.Millisecond), loaded.Downloaded, loaded.Title, len(loaded.Content))
			}

			t.Logf("candidate ok site=%s search=%s detail=%s chapter=%s results=%d chapters=%d book=%s/%s", tc.siteKey, searchElapsed.Round(time.Millisecond), detailElapsed.Round(time.Millisecond), chapterElapsed.Round(time.Millisecond), len(results), len(book.Chapters), tc.siteKey, book.ID)
		})
	}
}

func registerCandidateHealthSites(registry *Registry) {
	registry.RegisterWithHosts("westnovel", []string{"westnovel.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewWestNovelSite(cfg)
	})
	registry.RegisterWithHosts("piaotia", []string{"piaotia.com"}, func(cfg config.ResolvedSiteConfig) Site {
		return NewPiaotiaSite(cfg)
	})
}

func resolveCandidateHealthRefs(t *testing.T, client Site, tc candidateHealthCase) (model.BookRef, string) {
	t.Helper()
	bookRef, expectedChapterID := resolveManualHealthRefs(t, client, manualHealthCase{
		siteKey:    tc.siteKey,
		bookURL:    tc.bookURL,
		chapterURL: tc.chapterURL,
		chapterID:  tc.chapterID,
	})
	return bookRef, expectedChapterID
}

func validateCandidateBook(siteKey string, ref model.BookRef, book *model.Book) error {
	if book == nil {
		return fmt.Errorf("download plan returned nil book")
	}
	if strings.TrimSpace(book.ID) == "" {
		return fmt.Errorf("empty book id")
	}
	if book.ID != ref.BookID {
		return fmt.Errorf("unexpected book id: got %s want %s", book.ID, ref.BookID)
	}
	if strings.TrimSpace(book.Title) == "" {
		return fmt.Errorf("empty title")
	}
	if len(book.Chapters) == 0 {
		return fmt.Errorf("no chapters")
	}
	for idx, chapter := range book.Chapters {
		if strings.TrimSpace(chapter.ID) == "" {
			return fmt.Errorf("chapter %d has empty id", idx)
		}
		if strings.TrimSpace(chapter.Title) == "" {
			return fmt.Errorf("chapter %d has empty title", idx)
		}
	}
	_ = siteKey
	return nil
}

func firstCandidateChapter(chapters []model.Chapter, expectedID string) model.Chapter {
	if len(chapters) == 0 {
		return model.Chapter{}
	}
	if expectedID != "" {
		for _, chapter := range chapters {
			if chapter.ID == expectedID {
				return chapter
			}
		}
	}
	sorted := append([]model.Chapter(nil), chapters...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})
	return sorted[0]
}
