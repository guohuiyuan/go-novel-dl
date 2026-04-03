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
	)

	cmd := &cobra.Command{
		Use:           "novel-dl [keyword]",
		Short:         "交互式聚合搜索小说下载器",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			initialKeyword := ""
			if len(args) > 0 {
				initialKeyword = strings.TrimSpace(args[0])
			}
			return runInteractive(cmd.Context(), configPath, initialKeyword, sites)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "配置文件路径")
	cmd.Flags().StringSliceVarP(&sites, "site", "s", nil, "只在指定渠道中搜索；默认使用当前 Web 的默认渠道")

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

func runInteractive(ctx context.Context, configPath string, initialKeyword string, sites []string) error {
	return StartInteractiveUI(ctx, configPath, initialKeyword, sites)
}

func interactiveDownloadInput(runtime *app.Runtime) (string, []model.BookRef, error) {
	console := runtime.Console
	sites := runtime.Registry.Keys()
	idx, err := console.Select("选择渠道", sites)
	if err != nil {
		return "", nil, err
	}
	bookID, err := console.Prompt("小说 ID")
	if err != nil {
		return "", nil, err
	}
	start, err := console.Prompt("起始章节 ID（可选）")
	if err != nil {
		return "", nil, err
	}
	end, err := console.Prompt("结束章节 ID（可选）")
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
		return "", nil, fmt.Errorf("没有找到已下载的小说")
	}

	idx, err := console.Select("选择渠道", sites)
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
	selected, err := console.SelectMany("选择小说", labels)
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
