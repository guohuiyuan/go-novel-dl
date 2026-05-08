package site

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestParseN23QBSitemap(t *testing.T) {
	markup := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<item><title>Reborn Book</title><link>https://www.23qb.com/book/12713/</link><image>https://www.23qb.com/cover.jpg</image><author>Alice</author><pubDate>2026-05-08</pubDate><description><![CDATA[""]]></description></item>
<item><title>Duplicate</title><link>https://www.23qb.com/book/12713/</link><author>Bob</author></item>
</channel></rss>`

	results, err := parseN23QBSitemap(markup)
	if err != nil {
		t.Fatalf("parse sitemap: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one deduped result, got %d", len(results))
	}
	if results[0].BookID != "12713" || results[0].Title != "Reborn Book" || results[0].Author != "Alice" {
		t.Fatalf("unexpected sitemap result: %+v", results[0])
	}
}

func TestN23QBLiveSearch(t *testing.T) {
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_SEARCH") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_SEARCH=1 to run live n23qb search")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := NewN23QBSite(config.DefaultConfig().ResolveSiteConfig("n23qb")).Search(ctx, "重生", 5)
	if err != nil {
		t.Fatalf("live search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected live search results")
	}
	t.Logf("n23qb first result: %s/%s %s", results[0].Site, results[0].BookID, results[0].Title)
}
