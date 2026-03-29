package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/version"
)

func NewRootCmd() *cobra.Command {
	var (
		configPath string
		sites      []string
		allSites   bool
	)

	cmd := &cobra.Command{
		Use:           "novel-dl [keyword]",
		Short:         "Interactive hybrid-search novel downloader",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			initialKeyword := ""
			if len(args) > 0 {
				initialKeyword = strings.TrimSpace(args[0])
			}
			return runInteractive(cmd.Context(), configPath, initialKeyword, sites, allSites)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "Restrict interactive search to specific site key(s)")
	cmd.Flags().BoolVar(&allSites, "all-sites", false, "Use all web-visible searchable download sources instead of the default web source set")

	cmd.AddCommand(
		newDownloadCmd(),
		newSearchCmd(),
		newExportCmd(),
		newConfigCmd(),
		newCleanCmd(),
		newWebCmd(),
	)

	return cmd
}

func runInteractive(ctx context.Context, configPath string, initialKeyword string, sites []string, allSites bool) error {
	return StartInteractiveUI(ctx, configPath, initialKeyword, sites, allSites)
}

func interactiveDownloadInput(runtime *app.Runtime) (string, []model.BookRef, error) {
	console := runtime.Console
	sites := runtime.Registry.Keys()
	idx, err := console.Select("Choose site", sites)
	if err != nil {
		return "", nil, err
	}
	bookID, err := console.Prompt("Book ID")
	if err != nil {
		return "", nil, err
	}
	start, err := console.Prompt("Start chapter ID (optional)")
	if err != nil {
		return "", nil, err
	}
	end, err := console.Prompt("End chapter ID (optional)")
	if err != nil {
		return "", nil, err
	}
	return sites[idx], []model.BookRef{{BookID: strings.TrimSpace(bookID), StartID: strings.TrimSpace(start), EndID: strings.TrimSpace(end)}}, nil
}

func interactiveExportInput(runtime *app.Runtime) (string, []model.BookRef, error) {
	console := runtime.Console
	sites, err := runtime.ListStoredSites()
	if err != nil {
		return "", nil, err
	}
	if len(sites) == 0 {
		return "", nil, fmt.Errorf("no downloaded books found")
	}

	idx, err := console.Select("Choose site", sites)
	if err != nil {
		return "", nil, err
	}
	siteKey := sites[idx]
	books, err := runtime.ListStoredBooks(siteKey)
	if err != nil {
		return "", nil, err
	}
	labels := make([]string, 0, len(books))
	for _, book := range books {
		labels = append(labels, fmt.Sprintf("%s (%s)", book.Title, book.BookID))
	}
	selected, err := console.SelectMany("Choose books", labels)
	if err != nil {
		return "", nil, err
	}
	refs := make([]model.BookRef, 0, len(selected))
	for _, idx := range selected {
		refs = append(refs, model.BookRef{BookID: books[idx].BookID})
	}
	return siteKey, refs, nil
}

func normalizeLang(value string) string {
	switch strings.TrimSpace(value) {
	case "zh":
		return "zh_CN"
	case "en":
		return "en_US"
	default:
		return strings.TrimSpace(value)
	}
}
