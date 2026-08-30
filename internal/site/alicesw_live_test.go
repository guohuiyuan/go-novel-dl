//go:build live

package site

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestLiveAliceswSearch(t *testing.T) {
	s := NewAliceswSite(config.DefaultConfig().ResolveSiteConfig("alicesw"))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	results, err := s.Search(ctx, "重生", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	fmt.Printf("search -> n=%d\n", len(results))
	for i, r := range results {
		fmt.Printf("  [%d] id=%s title=%q author=%q latest=%q cover=%v\n",
			i, r.BookID, r.Title, r.Author, r.LatestChapter, r.CoverURL != "")
	}
}

func TestLiveAliceswDownload(t *testing.T) {
	s := NewAliceswSite(config.DefaultConfig().ResolveSiteConfig("alicesw"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	plan, err := s.DownloadPlan(ctx, model.BookRef{BookID: "36155"})
	if err != nil {
		t.Fatalf("DownloadPlan: %v", err)
	}
	fmt.Printf("plan -> %q / %q / chapters=%d cover=%v\n",
		plan.Title, plan.Author, len(plan.Chapters), plan.CoverURL != "")

	total := len(plan.Chapters)
	if total > 12 {
		total = 12
	}
	failed := 0
	short := 0
	for i := 0; i < total; i++ {
		ch, err := s.FetchChapter(ctx, "36155", plan.Chapters[i])
		if err != nil {
			failed++
			fmt.Printf("  ERR [%d] %s %s: %v\n", i, ch.ID, ch.Title, err)
			continue
		}
		if len([]rune(ch.Content)) < 200 {
			short++
			fmt.Printf("  SHORT [%d] %s %s len=%d\n", i, ch.ID, ch.Title, len([]rune(ch.Content)))
		}
	}
	fmt.Printf("checked %d chapters: failed=%d short=%d\n", total, failed, short)
	if failed > 0 || short > 0 {
		t.Fatalf("download check failed: failed=%d short=%d", failed, short)
	}
}

// TestLiveAliceswDumpChapter 排查章节页实际返回内容。
func TestLiveAliceswDumpChapter(t *testing.T) {
	if os.Getenv("ALICESW_DUMP") == "" {
		t.Skip("set ALICESW_DUMP=1 to dump chapter page")
	}
	s := NewAliceswSite(config.DefaultConfig().ResolveSiteConfig("alicesw"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	markup, err := s.getWithRetry(ctx, "https://www.alicesw1.homes/book/37685/1e0333e1b8841.html")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := os.WriteFile("alicesw_chapter_live.html", []byte(markup), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fmt.Printf("dumped %d bytes read-content=%v ps=%d\n",
		len(markup), strings.Contains(markup, "read-content"), strings.Count(markup, "<p"))
	title, paragraphs, err := parseAliceswChapterPage(markup)
	fmt.Printf("parse -> title=%q paragraphs=%d err=%v\n", title, len(paragraphs), err)
}

// TestLiveAliceswDumpSearchPage 排查搜索结果页结构，平时跳过。
func TestLiveAliceswDumpSearchPage(t *testing.T) {
	if os.Getenv("ALICESW_DUMP") == "" {
		t.Skip("set ALICESW_DUMP=1 to dump search page")
	}
	s := NewAliceswSite(config.DefaultConfig().ResolveSiteConfig("alicesw"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	markup, err := s.searchPage(ctx, "重生", 1)
	if err != nil {
		t.Fatalf("search page: %v", err)
	}
	if err := os.WriteFile("alicesw_search_live.html", []byte(markup), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fmt.Printf("dumped %d bytes\n", len(markup))
}
