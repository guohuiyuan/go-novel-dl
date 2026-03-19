package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/site"
)

func newDownloadCmd() *cobra.Command {
	var (
		siteKey    string
		configPath string
		startID    string
		endID      string
		noExport   bool
		formats    []string
	)

	cmd := &cobra.Command{
		Use:   "download [book_ids | url]",
		Short: "Download novels by book ID or URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			console := newConsole()
			runtime, _, err := loadRuntime(console, configPath)
			if err != nil {
				return err
			}
			if runtime == nil {
				return nil
			}

			books := parseBookRefs(args, startID, endID)
			if siteKey == "" {
				rawURL, err := ensureSingleURL(args)
				if err != nil {
					return err
				}
				console.Infof("No --site provided; detecting site from URL...")
				resolved, ok := site.ResolveURL(runtime.Registry, rawURL)
				if !ok || resolved.BookID == "" {
					return fmt.Errorf("could not resolve site and book from URL: %s", rawURL)
				}
				siteKey = resolved.SiteKey
				books = parseBookRefs([]string{resolved.BookID}, startID, endID)
				console.Infof("Resolved URL to site %q with book ID %q", siteKey, resolved.BookID)
			}

			console.Infof("Using site: %s", siteKey)
			results, err := runtime.Download(cmd.Context(), siteKey, books, formats, noExport)
			if err != nil {
				return err
			}
			for _, result := range results {
				console.Successf("Downloaded %s/%s -> stage %s", result.Book.Site, result.Book.ID, result.Stage)
				if noExport {
					continue
				}
				for _, path := range result.Exported {
					console.Infof("Exported %s", path)
				}
			}
			if len(results) == 0 {
				console.Warnf("No book IDs provided. Exiting.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&siteKey, "site", "", "Source site key (auto-detected if omitted and URL is provided)")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().StringVar(&startID, "start", "", "Start chapter ID (applies only to the first book)")
	cmd.Flags().StringVar(&endID, "end", "", "End chapter ID (applies only to the first book)")
	cmd.Flags().BoolVar(&noExport, "no-export", false, "Skip export step (download only)")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "Output format(s) (default: config)")
	return cmd
}
