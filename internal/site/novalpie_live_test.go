package site

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestNovalpieLiveFetchEncryptedChapter(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("NOVALPIE_TOKEN"))
	if token == "" {
		t.Skip("NOVALPIE_TOKEN is not set")
	}
	cfg := config.DefaultConfig().ResolveSiteConfig("novalpie")
	cfg.Cookie = "Bearer " + token
	cfg.General.Output.IncludePicture = true
	cfg.General.Timeout = 30
	site := NewNovalpieSite(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	chapter, err := site.FetchChapter(ctx, "1059", model.Chapter{ID: "245640"})
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if !chapter.Downloaded || strings.TrimSpace(chapter.Content) == "" {
		t.Fatalf("expected downloaded chapter content, got downloaded=%v len=%d", chapter.Downloaded, len(chapter.Content))
	}
	if strings.Contains(chapter.Content, "R1uSZb2K9") || strings.Contains(chapter.Content, "encrypted") {
		t.Fatalf("chapter content still looks encrypted: %.80q", chapter.Content)
	}
}
