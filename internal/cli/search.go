package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func newSearchCmd() *cobra.Command {
	var (
		sites      []string
		configPath string
		limit      int
		siteLimit  int
		timeout    float64
		formats    []string
		allSites   bool
	)

	cmd := &cobra.Command{
		Use:   "search keyword",
		Short: "Hybrid search books across one or more sites",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			console := newConsole()
			runtime, _, err := loadRuntime(console, configPath)
			if err != nil {
				return err
			}
			if runtime == nil {
				return nil
			}

			ctx, cancel := withTimeout(cmd.Context(), timeout)
			defer cancel()

			keyword := args[0]
			selectedSites := normalizeSites(sites)
			switch {
			case len(selectedSites) > 0:
			case allSites:
				selectedSites = allInteractiveSites(runtime)
			default:
				selectedSites = defaultInteractiveSites(runtime)
			}

			console.Infof("Hybrid searching for %q...", keyword)
			response, err := runtime.HybridSearch(ctx, keyword, app.HybridSearchOptions{
				Sites:        selectedSites,
				OverallLimit: limit,
				PerSiteLimit: siteLimit,
			})
			if err != nil {
				return err
			}
			if len(response.Results) == 0 {
				console.Warnf("No search results found")
				printHybridWarnings(console, response.Warnings)
				return nil
			}

			chapterCounts := loadHybridChapterCounts(ctx, runtime, response.Results)
			printHybridSearchResults(console, response.Results, chapterCounts)
			printHybridWarnings(console, response.Warnings)

			labels := make([]string, 0, len(response.Results))
			for _, result := range response.Results {
				labels = append(labels, fmt.Sprintf("[%s] %s (%s) - %s | %s | %s", result.PreferredSite, result.Title, result.Primary.BookID, result.Author, resultChapterCountLabel(result, chapterCounts), nonEmptyLatestChapter(result)))
			}
			selected, err := console.SelectMany("Choose result(s) to download", labels)
			if err != nil {
				return err
			}

			downloads, err := downloadHybridSelections(cmd.Context(), runtime, response.Results, selected, formats)
			if err != nil {
				return err
			}
			if len(downloads) == 0 {
				console.Warnf("No books were downloaded")
				return nil
			}
			for _, result := range downloads {
				console.Successf("Downloaded %s/%s", result.Book.Site, result.Book.ID)
				for _, path := range result.Exported {
					console.Infof("Exported %s", path)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "Restrict search to specific site key(s). Default: current web default sources")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Maximum number of total results")
	cmd.Flags().IntVar(&siteLimit, "site-limit", 10, "Maximum number of results per site")
	cmd.Flags().Float64Var(&timeout, "timeout", 5.0, "Request timeout in seconds")
	cmd.Flags().BoolVar(&allSites, "all-sites", false, "Use all web-visible searchable download sources instead of the default web source set")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "Output format(s) (default: config)")
	return cmd
}

func printHybridSearchResults(console interface{ Infof(string, ...any) }, results []app.HybridSearchResult, chapterCounts map[string]int) {
	for idx, result := range results {
		console.Infof("%d. [%s] %s (%s) - %s", idx+1, result.PreferredSite, result.Title, result.Primary.BookID, result.Author)
		console.Infof("   source: %s | chapters: %s | latest: %s", strings.Join(variantSites(result), ", "), resultChapterCountLabel(result, chapterCounts), nonEmptyLatestChapter(result))
	}
}

func printHybridWarnings(console interface{ Warnf(string, ...any) }, warnings []app.SearchWarning) {
	for _, warning := range warnings {
		console.Warnf("%s search failed: %s", warning.Site, warning.Error)
	}
}

func variantSites(result app.HybridSearchResult) []string {
	sites := make([]string, 0, len(result.Variants))
	for _, item := range result.Variants {
		sites = append(sites, item.Site)
	}
	return sites
}

func resultChapterCountLabel(result app.HybridSearchResult, chapterCounts map[string]int) string {
	if chapterCounts != nil {
		if count, ok := chapterCounts[hybridResultKey(result)]; ok && count > 0 {
			return fmt.Sprintf("%d", count)
		}
	}
	return "-"
}

func nonEmptyLatestChapter(result app.HybridSearchResult) string {
	if strings.TrimSpace(result.LatestChapter) == "" {
		return "-"
	}
	return result.LatestChapter
}

func hybridResultKey(result app.HybridSearchResult) string {
	return strings.TrimSpace(result.Primary.Site) + "|" + strings.TrimSpace(result.Primary.BookID)
}

func loadHybridChapterCounts(ctx context.Context, runtime *app.Runtime, results []app.HybridSearchResult) map[string]int {
	if runtime == nil || len(results) == 0 {
		return nil
	}

	limited := append([]app.HybridSearchResult(nil), results...)
	if len(limited) > chapterCountEnrichLimit {
		limited = limited[:chapterCountEnrichLimit]
	}

	counts := make(map[string]int, len(limited))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for _, result := range limited {
		result := result
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resolved := runtime.Config.ResolveSiteConfig(result.Primary.Site)
			client, err := runtime.Registry.Build(result.Primary.Site, resolved)
			if err != nil {
				return
			}

			itemCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()

			book, err := client.DownloadPlan(itemCtx, model.BookRef{BookID: result.Primary.BookID})
			if err != nil || book == nil || len(book.Chapters) == 0 {
				return
			}

			mu.Lock()
			counts[hybridResultKey(result)] = len(book.Chapters)
			mu.Unlock()
		}()
	}

	wg.Wait()
	return counts
}

func downloadHybridSelections(ctx context.Context, runtime *app.Runtime, results []app.HybridSearchResult, selected []int, formats []string) ([]app.DownloadResult, error) {
	if runtime == nil || len(results) == 0 || len(selected) == 0 {
		return nil, nil
	}

	sort.Ints(selected)
	orderedSites := make([]string, 0)
	siteBooks := make(map[string][]model.BookRef)
	seen := make(map[string]struct{})

	for _, idx := range selected {
		if idx < 0 || idx >= len(results) {
			continue
		}

		result := results[idx]
		siteKey := strings.TrimSpace(result.Primary.Site)
		bookID := strings.TrimSpace(result.Primary.BookID)
		if siteKey == "" || bookID == "" {
			continue
		}

		key := siteKey + "|" + bookID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if _, ok := siteBooks[siteKey]; !ok {
			orderedSites = append(orderedSites, siteKey)
		}
		siteBooks[siteKey] = append(siteBooks[siteKey], model.BookRef{BookID: bookID})
	}

	downloads := make([]app.DownloadResult, 0)
	for _, siteKey := range orderedSites {
		items, err := runtime.Download(ctx, siteKey, siteBooks[siteKey], formats, false)
		if err != nil {
			return downloads, err
		}
		downloads = append(downloads, items...)
	}
	return downloads, nil
}
