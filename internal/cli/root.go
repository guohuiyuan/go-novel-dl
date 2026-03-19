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
	cmd := &cobra.Command{
		Use:           "novel-cli",
		Short:         "CLI-first novel downloader scaffold in Go",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive(cmd.Context())
		},
	}

	cmd.AddCommand(
		newDownloadCmd(),
		newSearchCmd(),
		newExportCmd(),
		newConfigCmd(),
		newCleanCmd(),
	)

	return cmd
}

func runInteractive(ctx context.Context) error {
	console := newConsole()
	runtime, _, err := loadRuntime(console, "")
	if err != nil {
		return err
	}
	if runtime == nil {
		return nil
	}

	options := []string{
		"download",
		"search",
		"export",
		"config init",
		"config set-lang",
		"clean logs",
		"quit",
	}

	choice, err := console.Select("Select a command", options)
	if err != nil {
		return err
	}

	switch options[choice] {
	case "download":
		siteKey, refs, err := interactiveDownloadInput(runtime)
		if err != nil {
			return err
		}
		results, err := runtime.Download(ctx, siteKey, refs, nil, false)
		if err != nil {
			return err
		}
		for _, result := range results {
			console.Successf("Downloaded %s/%s", result.Book.Site, result.Book.ID)
			for _, path := range result.Exported {
				console.Infof("Exported %s", path)
			}
		}
	case "search":
		keyword, err := console.Prompt("Keyword")
		if err != nil {
			return err
		}
		results, err := runtime.Search(ctx, nil, keyword, 20, 5)
		if err != nil {
			return err
		}
		console.PrintSearchResults(results)
	case "export":
		siteKey, refs, err := interactiveExportInput(runtime)
		if err != nil {
			return err
		}
		paths, err := runtime.Export(siteKey, refs, "", nil)
		if err != nil {
			return err
		}
		for _, path := range paths {
			console.Successf("Exported %s", path)
		}
	case "config init":
		if err := configInit(runtime, false); err != nil {
			return err
		}
	case "config set-lang":
		lang, err := console.Prompt("Language (zh, zh_CN, en, en_US)")
		if err != nil {
			return err
		}
		lang = normalizeLang(lang)
		if err := runtime.State.SetLanguage(lang); err != nil {
			return err
		}
		console.Successf("Language switched to %s", lang)
	case "clean logs":
		removed, err := runtime.CleanLogs(false)
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			console.Infof("No log files to clean")
		}
		for _, path := range removed {
			console.Successf("Removed %s", path)
		}
	case "quit":
		console.Infof("Bye")
	}

	return nil
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
