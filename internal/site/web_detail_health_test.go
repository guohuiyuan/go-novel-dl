package site

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type webDetailHealthCase struct {
	siteKey string
	keyword string
	timeout time.Duration
}

func TestManualWebSourceDetailHealth(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_WEB_DETAIL_HEALTH") == "" {
		t.Skip("set GO_NOVEL_DL_WEB_DETAIL_HEALTH=1 to run manual web-source detail health checks")
	}

	cfg := loadManualHealthConfig(t)
	registry := NewDefaultRegistry()
	filter := parseManualHealthFilter(os.Getenv("GO_NOVEL_DL_HEALTH_SITES"))

	testCases := []webDetailHealthCase{
		{siteKey: "ciweimao", keyword: "Gal姝﹀湥", timeout: 90 * time.Second},
		{siteKey: "ciyuanji", keyword: "鏂楃綏", timeout: 90 * time.Second},
		{siteKey: "faloo", keyword: "鍘熺锛氬姞鎶ゅ湪韬?", timeout: 2 * time.Minute},
		{siteKey: "fsshu", keyword: "浠栦滑瓒婂弽瀵癸紝瓒婃槸璇存槑鎴戝仛瀵逛簡", timeout: 90 * time.Second},
		{siteKey: "ixdzs8", keyword: "斗罗", timeout: 90 * time.Second},
		{siteKey: "linovelib", keyword: "闅愬尶鐨勫瓨鍦?", timeout: 3 * time.Minute},
		{siteKey: "n17k", keyword: "璇＄涔嬪湴", timeout: 90 * time.Second},
		{siteKey: "n8novel", keyword: "斗罗", timeout: 2 * time.Minute},
		{siteKey: "n23qb", keyword: "寰″吔", timeout: 3 * time.Minute},
		{siteKey: "ruochu", keyword: "鎬昏", timeout: 90 * time.Second},
		{siteKey: "sfacg", keyword: "灏戝コ", timeout: 2 * time.Minute},
	}

	for _, tc := range testCases {
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
			if resolvedCfg.General.RetryTimes < 2 {
				resolvedCfg.General.RetryTimes = 2
			}

			client, err := registry.Build(tc.siteKey, resolvedCfg)
			if err != nil {
				t.Fatalf("build site %s: %v", tc.siteKey, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			results, err := client.Search(ctx, tc.keyword, 5)
			if err != nil {
				t.Fatalf("search %s with %q: %v", tc.siteKey, tc.keyword, err)
			}
			if len(results) == 0 {
				t.Fatalf("search %s with %q returned no results", tc.siteKey, tc.keyword)
			}
			sort.SliceStable(results, func(i, j int) bool {
				return webDetailCandidateScore(results[i], tc.keyword) > webDetailCandidateScore(results[j], tc.keyword)
			})

			var success *model.Book
			var successResult model.SearchResult
			failures := make([]string, 0, len(results))
			for idx, item := range results {
				if idx >= 5 {
					break
				}
				book, err := client.DownloadPlan(ctx, model.BookRef{BookID: item.BookID})
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s: %v", item.BookID, err))
					continue
				}
				if err := validateWebDetailResult(tc.siteKey, item, book); err != nil {
					failures = append(failures, fmt.Sprintf("%s: %v", item.BookID, err))
					continue
				}
				success = book
				successResult = item
				break
			}

			if success == nil {
				t.Fatalf("no valid detail result for %s after %d candidates: %s", tc.siteKey, minInt(5, len(results)), strings.Join(failures, "; "))
			}

			t.Logf("detail ok for %s/%s: search=%q title=%q chapters=%d latest=%q", tc.siteKey, success.ID, tc.keyword, firstNonEmpty(success.Title, successResult.Title), len(success.Chapters), successResult.LatestChapter)
		})
	}
}

func validateWebDetailResult(siteKey string, result model.SearchResult, book *model.Book) error {
	if book == nil {
		return fmt.Errorf("download plan returned nil book")
	}
	if strings.TrimSpace(book.ID) == "" {
		return fmt.Errorf("empty book id")
	}
	if book.ID != result.BookID {
		return fmt.Errorf("unexpected book id: got %s want %s", book.ID, result.BookID)
	}
	if strings.TrimSpace(firstNonEmpty(book.Title, result.Title)) == "" {
		return fmt.Errorf("empty title")
	}
	if strings.TrimSpace(firstNonEmpty(book.Author, result.Author)) == "" {
		return fmt.Errorf("empty author")
	}
	if strings.TrimSpace(firstNonEmpty(book.Description, result.Description)) == "" {
		return fmt.Errorf("empty description")
	}
	if strings.TrimSpace(firstNonEmpty(book.SourceURL, result.URL)) == "" {
		return fmt.Errorf("empty source url")
	}
	if strings.TrimSpace(result.LatestChapter) == "" {
		return fmt.Errorf("empty latest chapter")
	}
	if len(book.Chapters) == 0 {
		return fmt.Errorf("no chapters returned")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

func webDetailCandidateScore(result model.SearchResult, keyword string) int {
	keywordNorm := normalizeWebDetailText(keyword)
	titleNorm := normalizeWebDetailText(result.Title)
	authorNorm := normalizeWebDetailText(result.Author)
	descNorm := normalizeWebDetailText(result.Description)

	score := 0
	switch {
	case keywordNorm != "" && titleNorm == keywordNorm:
		score += 100
	case keywordNorm != "" && strings.Contains(titleNorm, keywordNorm):
		score += 80
	case keywordNorm != "" && strings.Contains(keywordNorm, titleNorm):
		score += 70
	}
	if keywordNorm != "" && strings.Contains(authorNorm, keywordNorm) {
		score += 20
	}
	if keywordNorm != "" && strings.Contains(descNorm, keywordNorm) {
		score += 10
	}
	if strings.TrimSpace(result.Description) != "" {
		score += 5
	}
	return score
}

func normalizeWebDetailText(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.In(r, unicode.Han) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
