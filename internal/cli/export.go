package cli

import "github.com/spf13/cobra"

func newExportCmd() *cobra.Command {
	var (
		siteKey    string
		configPath string
		startID    string
		endID      string
		stage      string
		formats    []string
	)

	cmd := &cobra.Command{
		Use:   "export [book_id ...]",
		Short: "导出已下载的小说",
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
				selectedSite, selectedBooks, err := interactiveExportInput(runtime)
				if err != nil {
					return err
				}
				siteKey = selectedSite
				books = selectedBooks
			}

			paths, err := runtime.Export(siteKey, books, stage, formats)
			if err != nil {
				return err
			}
			if len(paths) == 0 {
				console.Warnf("没有导出任何书籍")
				return nil
			}
			for _, path := range paths {
				console.Successf("已导出 %s", path)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&formats, "format", nil, "导出格式列表，默认读取配置")
	cmd.Flags().StringVar(&siteKey, "site", "", "渠道 key；省略时进入交互选择")
	cmd.Flags().StringVar(&configPath, "config", "", "配置文件路径")
	cmd.Flags().StringVar(&startID, "start", "", "起始章节 ID（仅作用于第一本书）")
	cmd.Flags().StringVar(&endID, "end", "", "结束章节 ID（仅作用于第一本书）")
	cmd.Flags().StringVar(&stage, "stage", "", "导出阶段，默认使用最新阶段")
	return cmd
}
