package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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
		Short: "Search for books across one or more sites",
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
			console.Infof("Searching for %q...", keyword)
			results, err := runtime.Search(ctx, normalizeSites(sites), keyword, limit, siteLimit)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				console.Warnf("No search results found")
				return nil
			}

			console.PrintSearchResults(results)
			labels := make([]string, 0, len(results))
			for _, result := range results {
				labels = append(labels, fmt.Sprintf("[%s] %s (%s)", result.Site, result.Title, result.BookID))
			}
			selected, err := console.Select("Choose a result to download", labels)
			if err != nil {
				return err
			}
			chosen := results[selected]

			downloads, err := runtime.Download(cmd.Context(), chosen.Site, []model.BookRef{{BookID: chosen.BookID}}, formats, false)
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

	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "Restrict search to specific site key(s). Default: all registered sites")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Maximum number of total results")
	cmd.Flags().IntVar(&siteLimit, "site-limit", 10, "Maximum number of results per site")
	cmd.Flags().Float64Var(&timeout, "timeout", 5.0, "Request timeout in seconds")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "Output format(s) (default: config)")
	return cmd
}
