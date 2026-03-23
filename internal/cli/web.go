package cli

import (
	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/web"
)

func newWebCmd() *cobra.Command {
	var (
		port       string
		noBrowser  bool
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the Web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return web.Start(port, !noBrowser, configPath)
		},
	}

	cmd.Flags().StringVarP(&port, "port", "p", "8080", "Web server port")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open a browser automatically")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	return cmd
}
