package site

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestIntegrationDownloadAvailableSites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration download test in short mode")
	}
	if os.Getenv("GO_NOVEL_DL_INTEGRATION_DOWNLOAD") == "" {
		t.Skip("set GO_NOVEL_DL_INTEGRATION_DOWNLOAD=1 to run live integration download tests")
	}

	registry := NewDefaultRegistry()
	defaults := config.DefaultConfig()
	defaults.General.Timeout = 20
	defaults.General.RequestInterval = 0.2
	defaults.General.RetryTimes = 1

	testCases := []struct {
		name        string
		siteKey     string
		ref         model.BookRef
		minChapters int
	}{
		{
			name:        "fanqienovel",
			siteKey:     "fanqienovel",
			ref:         model.BookRef{BookID: "7276384138653862966", StartID: "7276442459937571380", EndID: "7276442459937571380"},
			minChapters: 1,
		},
		{
			name:        "faloo",
			siteKey:     "faloo",
			ref:         model.BookRef{BookID: "1482723", StartID: "1", EndID: "1"},
			minChapters: 1,
		},
		{
			name:        "sfacg",
			siteKey:     "sfacg",
			ref:         model.BookRef{BookID: "456123", StartID: "5417665", EndID: "5417665"},
			minChapters: 1,
		},
		{
			name:        "ciweimao",
			siteKey:     "ciweimao",
			ref:         model.BookRef{BookID: "100011781", StartID: "100257072", EndID: "100257072"},
			minChapters: 1,
		},
		{
			name:        "tongrenshe",
			siteKey:     "tongrenshe",
			ref:         model.BookRef{BookID: "8899", StartID: "1", EndID: "1"},
			minChapters: 1,
		},
		{
			name:        "wenku8",
			siteKey:     "wenku8",
			ref:         model.BookRef{BookID: "2835", StartID: "113354", EndID: "113354"},
			minChapters: 1,
		},
		{
			name:        "qbtr",
			siteKey:     "qbtr",
			ref:         model.BookRef{BookID: "tongren-8978", StartID: "1", EndID: "1"},
			minChapters: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.siteKey == "qbtr" {
				t.Skip("qbtr is intermittently unstable in CI/local network; run manually when needed")
			}

			cfg := defaults.ResolveSiteConfig(tc.siteKey)
			site, err := registry.Build(tc.siteKey, cfg)
			if err != nil {
				t.Fatalf("build site %s: %v", tc.siteKey, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			book, err := site.Download(ctx, tc.ref)
			if err != nil {
				t.Fatalf("download %s/%s: %v", tc.siteKey, tc.ref.BookID, err)
			}
			if book == nil {
				t.Fatalf("download %s/%s returned nil book", tc.siteKey, tc.ref.BookID)
			}
			if book.Site != tc.siteKey {
				t.Fatalf("unexpected site: got %s want %s", book.Site, tc.siteKey)
			}
			if book.ID != tc.ref.BookID {
				t.Fatalf("unexpected book id: got %s want %s", book.ID, tc.ref.BookID)
			}
			if len(book.Chapters) < tc.minChapters {
				t.Fatalf("downloaded chapters too few: got %d want >= %d", len(book.Chapters), tc.minChapters)
			}

			for i, chapter := range book.Chapters {
				if !chapter.Downloaded {
					t.Fatalf("chapter %d not marked downloaded: %+v", i, chapter)
				}
				if chapter.Title == "" {
					t.Fatalf("chapter %d has empty title", i)
				}
				if chapter.Content == "" {
					t.Fatalf("chapter %d has empty content", i)
				}
			}
		})
	}
}
