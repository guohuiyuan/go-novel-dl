package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func newSearchCmd() *cobra.Command {
	var (
		sites      []string
		configPath string
		limit      int
		siteLimit  int
		pageSize   int
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

			effectivePageSize := pageSize
			if effectivePageSize <= 0 {
				effectivePageSize = runtime.Config.General.CLIPageSize
			}
			if effectivePageSize <= 0 {
				effectivePageSize = defaultCLISearchPageSize
			}

			keyword := args[0]
			selectedSites := normalizeSites(sites)
			if len(selectedSites) == 0 {
				selectedSites = interactiveSites(runtime)
			}
			effectiveTimeout := timeout
			if !cmd.Flags().Changed("timeout") {
				if siteTimeout := searchTimeoutSecondsForSites(selectedSites); siteTimeout > effectiveTimeout {
					effectiveTimeout = siteTimeout
				}
			}

			ctx, cancel := withTimeout(cmd.Context(), effectiveTimeout)
			defer cancel()

			if shouldRequireESJAuthForSearch(selectedSites, runtime.AllSearchSites()) {
				resolved := runtime.Config.ResolveSiteConfig("esjzone")
				if strings.TrimSpace(resolved.Cookie) == "" && strings.TrimSpace(resolved.Password) == "" {
					return fmt.Errorf("ESJ Zone 未配置 Cookie 或密码，请先执行 config site-set esjzone ...")
				}
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

			chapterCounts := loadHybridChapterCounts(ctx, runtime, response.Results, effectivePageSize)
			printHybridWarnings(console, response.Warnings)

			selected, err := selectHybridResultsPaged(ctx, runtime, console, response.Results, chapterCounts, effectivePageSize)
			if err != nil {
				return err
			}
			if len(selected) == 0 {
				console.Warnf("已取消下载选择")
				return nil
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

	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "只搜索指定渠道；默认使用当前 Web 的默认渠道")
	cmd.Flags().StringVar(&configPath, "config", "", "配置文件路径")
	cmd.Flags().IntVarP(&limit, "limit", "l", defaultSearchResultLimit, "总结果数上限")
	cmd.Flags().IntVar(&siteLimit, "site-limit", defaultCLISearchPageSize, "单渠道结果数上限")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "每页显示数量，默认读取配置")
	cmd.Flags().Float64Var(&timeout, "timeout", 5.0, "请求超时秒数")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "导出格式列表，默认读取配置")
	return cmd
}

func shouldRequireESJAuthForSearch(selectedSites, fallbackSites []string) bool {
	items := selectedSites
	if len(items) == 0 {
		items = fallbackSites
	}
	if len(items) != 1 {
		return false
	}
	for _, siteKey := range items {
		if strings.EqualFold(strings.TrimSpace(siteKey), "esjzone") {
			return true
		}
	}
	return false
}

func selectHybridResultsPaged(ctx context.Context, runtime *app.Runtime, console *ui.Console, results []app.HybridSearchResult, chapterCounts map[string]int, pageSize int) ([]int, error) {
	if len(results) == 0 {
		return nil, nil
	}

	page := 0
	selected := make(map[int]struct{})
	totalPages := resultPageCount(len(results), pageSize)
	for {
		ensureHybridSearchPageCounts(ctx, runtime, results, page, chapterCounts, pageSize)
		printHybridSearchPage(console, results, chapterCounts, page, selected, pageSize)

		input, err := console.Prompt("输入当前页序号切换选择，n 下一页，p 上一页，a 全选，c 清空，d 下载，q 退出")
		if err != nil {
			return nil, err
		}

		switch strings.ToLower(strings.TrimSpace(input)) {
		case "":
			continue
		case "n":
			if page >= totalPages-1 {
				console.Warnf("已经是最后一页")
				continue
			}
			page++
		case "p":
			if page <= 0 {
				console.Warnf("已经是第一页")
				continue
			}
			page--
		case "a":
			for idx := range results {
				selected[idx] = struct{}{}
			}
			console.Infof("已全选 %d 项", len(selected))
		case "c":
			selected = make(map[int]struct{})
			console.Infof("已清空选择")
		case "d":
			if len(selected) == 0 {
				console.Warnf("至少需要选择一项")
				continue
			}
			return sortedSelectionIndices(selected), nil
		case "q":
			return nil, nil
		default:
			if err := toggleHybridSearchPageSelection(input, page, len(results), selected, pageSize); err != nil {
				console.Warnf("%s", err)
				continue
			}
			console.Infof("已选择 %d 项", len(selected))
		}
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

func printHybridSearchPage(console interface{ Infof(string, ...any) }, results []app.HybridSearchResult, chapterCounts map[string]int, page int, selected map[int]struct{}, pageSize int) {
	totalPages := resultPageCount(len(results), pageSize)
	start, end := resultPageBounds(len(results), page, pageSize)
	console.Infof("第 %d/%d 页，每页 %d 条，共 %d 条结果", page+1, totalPages, pageSize, len(results))
	for idx := start; idx < end; idx++ {
		result := results[idx]
		checkText := "[ ]"
		if _, ok := selected[idx]; ok {
			checkText = "[x]"
		}
		console.Infof("%s %d. [%s] %s (%s) - %s", checkText, idx-start+1, result.PreferredSite, result.Title, result.Primary.BookID, result.Author)
		console.Infof("   渠道: %s | 章节数: %s | 最新章节: %s", strings.Join(variantSites(result), ", "), resultChapterCountLabel(result, chapterCounts), nonEmptyLatestChapter(result))
	}
	console.Infof("已选择 %d/%d 项", len(selected), len(results))
}

func toggleHybridSearchPageSelection(input string, page, total int, selected map[int]struct{}, pageSize int) error {
	start, end := resultPageBounds(total, page, pageSize)
	pageItemCount := end - start
	if pageItemCount <= 0 {
		return fmt.Errorf("当前页没有结果")
	}

	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		choice, err := strconv.Atoi(part)
		if err != nil || choice < 1 || choice > pageItemCount {
			return fmt.Errorf("当前页序号 %q 必须在 1 到 %d 之间", part, pageItemCount)
		}

		idx := start + choice - 1
		if _, ok := selected[idx]; ok {
			delete(selected, idx)
			continue
		}
		selected[idx] = struct{}{}
	}
	return nil
}

func sortedSelectionIndices(selected map[int]struct{}) []int {
	indices := make([]int, 0, len(selected))
	for idx := range selected {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	return indices
}

func ensureHybridSearchPageCounts(ctx context.Context, runtime *app.Runtime, results []app.HybridSearchResult, page int, chapterCounts map[string]int, pageSize int) {
	if runtime == nil || chapterCounts == nil {
		return
	}
	start, end := resultPageBounds(len(results), page, pageSize)
	if start >= end {
		return
	}

	pending := make([]app.HybridSearchResult, 0, end-start)
	for _, result := range results[start:end] {
		if _, ok := chapterCounts[hybridResultKey(result)]; ok {
			continue
		}
		pending = append(pending, result)
	}
	if len(pending) == 0 {
		return
	}

	loaded := loadHybridChapterCounts(ctx, runtime, pending, pageSize)
	for key, count := range loaded {
		chapterCounts[key] = count
	}
}

func loadHybridChapterCounts(ctx context.Context, runtime *app.Runtime, results []app.HybridSearchResult, pageSize int) map[string]int {
	if runtime == nil || len(results) == 0 {
		return nil
	}

	limited := append([]app.HybridSearchResult(nil), results...)
	if len(limited) > pageSize {
		limited = limited[:pageSize]
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
			if resolved.General.LoginRequired {
				return
			}
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
