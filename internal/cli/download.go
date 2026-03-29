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
		Short: "按书籍 ID 或 URL 下载小说",
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
				console.Infof("未提供 --site，正在从 URL 自动识别渠道...")
				resolved, ok := site.ResolveURL(runtime.Registry, rawURL)
				if !ok || resolved.BookID == "" {
					return fmt.Errorf("无法从 URL 解析出渠道和书籍：%s", rawURL)
				}
				siteKey = resolved.SiteKey
				books = parseBookRefs([]string{resolved.BookID}, startID, endID)
				console.Infof("已识别为渠道 %q，书籍 ID 为 %q", siteKey, resolved.BookID)
			}

			console.Infof("使用渠道：%s", siteKey)
			results, err := runtime.Download(cmd.Context(), siteKey, books, formats, noExport)
			if err != nil {
				return err
			}
			for _, result := range results {
				console.Successf("已下载 %s/%s，当前阶段：%s", result.Book.Site, result.Book.ID, result.Stage)
				if noExport {
					continue
				}
				for _, path := range result.Exported {
					console.Infof("已导出 %s", path)
				}
			}
			if len(results) == 0 {
				console.Warnf("没有提供书籍 ID，已结束。")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&siteKey, "site", "", "渠道 key；如果省略且传入 URL，会自动识别")
	cmd.Flags().StringVar(&configPath, "config", "", "配置文件路径")
	cmd.Flags().StringVar(&startID, "start", "", "起始章节 ID（仅作用于第一本书）")
	cmd.Flags().StringVar(&endID, "end", "", "结束章节 ID（仅作用于第一本书）")
	cmd.Flags().BoolVar(&noExport, "no-export", false, "跳过导出步骤，只下载")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "导出格式列表，默认读取配置")
	return cmd
}
