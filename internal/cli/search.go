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
	)

	cmd := &cobra.Command{
		Use:   "search keyword",
		Short: "跨多个渠道聚合搜索小说",
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
			if len(selectedSites) == 0 {
				selectedSites = interactiveSites(runtime)
			}

			console.Infof("正在聚合搜索 %q...", keyword)
			response, err := runtime.HybridSearch(ctx, keyword, app.HybridSearchOptions{
				Sites:        selectedSites,
				OverallLimit: limit,
				PerSiteLimit: siteLimit,
			})
			if err != nil {
				return err
			}
			if len(response.Results) == 0 {
				console.Warnf("没有找到搜索结果")
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
			selected, err := console.SelectMany("选择要下载的小说", labels)
			if err != nil {
				return err
			}

			downloads, err := downloadHybridSelections(cmd.Context(), runtime, response.Results, selected, formats)
			if err != nil {
				return err
			}
			if len(downloads) == 0 {
				console.Warnf("没有下载到任何小说")
				return nil
			}
			for _, result := range downloads {
				console.Successf("已下载 %s/%s", result.Book.Site, result.Book.ID)
				for _, path := range result.Exported {
					console.Infof("已导出 %s", path)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "只搜索指定渠道；默认直接使用当前 Web 的 9 个渠道")
	cmd.Flags().StringVar(&configPath, "config", "", "配置文件路径")
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "总结果数上限")
	cmd.Flags().IntVar(&siteLimit, "site-limit", 10, "单渠道结果数上限")
	cmd.Flags().Float64Var(&timeout, "timeout", 5.0, "请求超时秒数")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "导出格式列表，默认读取配置")
	return cmd
}

func printHybridSearchResults(console interface{ Infof(string, ...any) }, results []app.HybridSearchResult, chapterCounts map[string]int) {
	for idx, result := range results {
		console.Infof("%d. [%s] %s (%s) - %s", idx+1, result.PreferredSite, result.Title, result.Primary.BookID, result.Author)
		console.Infof("   渠道: %s | 章节数: %s | 最新章节: %s", strings.Join(variantSites(result), ", "), resultChapterCountLabel(result, chapterCounts), nonEmptyLatestChapter(result))
	}
}

func printHybridWarnings(console interface{ Warnf(string, ...any) }, warnings []app.SearchWarning) {
	for _, warning := range warnings {
		console.Warnf("%s 搜索失败: %s", warning.Site, warning.Error)
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
