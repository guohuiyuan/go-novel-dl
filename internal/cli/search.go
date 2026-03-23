package cli

import (
	"fmt"
	"strings"

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
			if len(selectedSites) == 0 && allSites {
				selectedSites = runtime.AllSearchSites()
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

			printHybridSearchResults(console, response.Results)
			printHybridWarnings(console, response.Warnings)

			labels := make([]string, 0, len(response.Results))
			for _, result := range response.Results {
				labels = append(labels, fmt.Sprintf("[%s] %s (%s) - %s", result.PreferredSite, result.Title, result.Primary.BookID, strings.Join(variantSites(result), ", ")))
			}
			selected, err := console.Select("Choose a result to download", labels)
			if err != nil {
				return err
			}
			chosen := response.Results[selected]

			downloads, err := runtime.Download(cmd.Context(), chosen.Primary.Site, []model.BookRef{{BookID: chosen.Primary.BookID}}, formats, false)
			if err != nil {
				return err
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

	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "Restrict search to specific site key(s). Default: current default available sources")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Maximum number of total results")
	cmd.Flags().IntVar(&siteLimit, "site-limit", 10, "Maximum number of results per site")
	cmd.Flags().Float64Var(&timeout, "timeout", 5.0, "Request timeout in seconds")
	cmd.Flags().BoolVar(&allSites, "all-sites", false, "Use all searchable sites instead of default available sources")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "Output format(s) (default: config)")
	return cmd
}

func printHybridSearchResults(console interface{ Infof(string, ...any) }, results []app.HybridSearchResult) {
	for idx, result := range results {
		console.Infof("%d. [%s] %s (%s) - %s", idx+1, result.PreferredSite, result.Title, result.Primary.BookID, result.Author)
		if result.Description != "" {
			console.Infof("   %s", result.Description)
		}
		console.Infof("   sources: %s", strings.Join(variantSites(result), ", "))
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
