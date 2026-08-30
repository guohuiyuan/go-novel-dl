//go:build live

package site

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestLiveESJZoneSearch(t *testing.T) {
	s := NewESJZoneSite(config.DefaultConfig().ResolveSiteConfig("esjzone"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
}

func TestLiveESJZoneDownload(t *testing.T) {
	s := NewESJZoneSite(config.DefaultConfig().ResolveSiteConfig("esjzone"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	bookID := "1706515594"
	plan, err := s.DownloadPlan(ctx, model.BookRef{BookID: bookID})
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
		ch, err := s.FetchChapter(ctx, bookID, plan.Chapters[i])
		if err != nil {
			failed++
			fmt.Printf("  ERR [%d] %s %s: %v\n", i, ch.ID, ch.Title, err)
			continue
		}
		if len([]rune(ch.Content)) < 20 {
			short++
			fmt.Printf("  SHORT [%d] %s %s len=%d\n", i, ch.ID, ch.Title, len([]rune(ch.Content)))
		}
	}
	fmt.Printf("checked %d chapters: failed=%d short=%d\n", total, failed, short)
	if failed > 0 || short > 0 {
		t.Fatalf("download check failed: failed=%d short=%d", failed, short)
	}
}
