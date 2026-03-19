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
		Short: "Export previously downloaded novels",
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
				console.Warnf("No books exported")
				return nil
			}
			for _, path := range paths {
				console.Successf("Exported %s", path)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&formats, "format", nil, "Output format(s) (default: config)")
	cmd.Flags().StringVar(&siteKey, "site", "", "Source site key (optional; choose interactively if omitted)")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().StringVar(&startID, "start", "", "Start chapter ID (applies only to the first book)")
	cmd.Flags().StringVar(&endID, "end", "", "End chapter ID (applies only to the first book)")
	cmd.Flags().StringVar(&stage, "stage", "", "Export stage (default: latest stage)")
	return cmd
}
