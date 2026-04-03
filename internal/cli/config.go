package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage application configuration and settings",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigSetLangCmd(), newConfigSitesCmd(), newConfigSiteSetCmd())
	return cmd
}

func newConfigSitesCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "sites",
		Short: "List managed site configuration in SQLite",
		RunE: func(cmd *cobra.Command, args []string) error {
			console := newConsole()
			if _, _, err := loadRuntime(console, configPath); err != nil {
				return err
			}

			items, err := config.ListSiteCatalog()
			if err != nil {
				return err
			}
			console.Infof("Site DB: %s", config.SiteCatalogPath())
			for _, item := range items {
				fields := make([]string, 0, 4)
				if strings.TrimSpace(item.Username) != "" {
					fields = append(fields, "username")
				}
				if strings.TrimSpace(item.Password) != "" {
					fields = append(fields, "password")
				}
				if strings.TrimSpace(item.Cookie) != "" {
					fields = append(fields, "cookie")
				}
				if len(item.MirrorHosts) > 0 {
					fields = append(fields, "mirror_hosts")
				}
				if strings.TrimSpace(item.LocaleStyle) != "" {
					fields = append(fields, "locale_style="+strings.TrimSpace(item.LocaleStyle))
				}
				if len(fields) == 0 {
					fields = append(fields, "none")
				}
				console.Infof("- %s (%s) login_required=%v configured=[%s]", item.Key, item.DisplayName, item.LoginRequired, strings.Join(fields, ", "))
			}

			supports := config.SiteParameterSupports()
			console.Infof("Implemented parameters:")
			for _, support := range supports {
				console.Infof("  - %s: %v (%s)", support.Key, support.Implemented, support.Notes)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	return cmd
}

func newConfigSiteSetCmd() *cobra.Command {
	var (
		configPath    string
		loginRequired bool
		setLogin      bool
		workerLimit   int
		setWorkers    bool
		fetchImages   bool
		setImages     bool
		localeStyle   string
		username      string
		password      string
		cookie        string
		mirrors       []string
	)

	cmd := &cobra.Command{
		Use:   "site-set SITE_KEY",
		Short: "Update one managed site config in SQLite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			console := newConsole()
			if _, _, err := loadRuntime(console, configPath); err != nil {
				return err
			}

			siteKey := strings.TrimSpace(args[0])
			if siteKey == "" {
				return fmt.Errorf("site key is required")
			}

			update := config.SiteCatalogUpdate{}
			if cmd.Flags().Changed("login-required") {
				setLogin = true
			}
			if setLogin {
				update.LoginRequired = &loginRequired
			}
			if cmd.Flags().Changed("workers") {
				setWorkers = true
			}
			if setWorkers {
				update.WorkerLimit = &workerLimit
			}
			if cmd.Flags().Changed("fetch-images") {
				setImages = true
			}
			if setImages {
				update.FetchImages = &fetchImages
			}
			if cmd.Flags().Changed("locale-style") {
				value := strings.TrimSpace(localeStyle)
				update.LocaleStyle = &value
			}
			if cmd.Flags().Changed("username") {
				value := username
				update.Username = &value
			}
			if cmd.Flags().Changed("password") {
				value := password
				update.Password = &value
			}
			if cmd.Flags().Changed("cookie") {
				value := cookie
				update.Cookie = &value
			}
			if cmd.Flags().Changed("mirror") {
				value := mirrors
				update.MirrorHosts = &value
			}

			item, err := config.UpsertSiteCatalog(siteKey, update)
			if err != nil {
				return err
			}
			console.Successf("Updated %s: login_required=%v workers=%d fetch_images=%v locale_style=%q username=%q mirrors=%d cookie=%v password=%v", item.Key, item.LoginRequired, item.WorkerLimit, item.FetchImages, item.LocaleStyle, item.Username, len(item.MirrorHosts), item.Cookie != "", item.Password != "")
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to the configuration file")
	cmd.Flags().BoolVar(&loginRequired, "login-required", false, "Whether this site requires login")
	cmd.Flags().IntVar(&workerLimit, "workers", 0, "Per-site chapter download worker limit (0 means fallback)")
	cmd.Flags().BoolVar(&fetchImages, "fetch-images", true, "Whether to keep/fetch images in chapter content")
	cmd.Flags().StringVar(&localeStyle, "locale-style", "", "Locale style for this site: original/traditional/simplified")
	cmd.Flags().StringVar(&username, "username", "", "Username for site login")
	cmd.Flags().StringVar(&password, "password", "", "Password for site login")
	cmd.Flags().StringVar(&cookie, "cookie", "", "Cookie header for site requests")
	cmd.Flags().StringSliceVar(&mirrors, "mirror", nil, "Mirror hosts, repeatable")
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize SQLite-backed configuration database",
		RunE: func(cmd *cobra.Command, args []string) error {
			console := newConsole()
			runtime := app.NewRuntime(configPtr(), console)
			return configInit(runtime, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite if the file already exists")
	return cmd
}

func newConfigSetLangCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-lang LANG",
		Short: "Set the interface language",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			console := newConsole()
			runtime := app.NewRuntime(configPtr(), console)
			lang := normalizeLang(args[0])
			if err := runtime.State.SetLanguage(lang); err != nil {
				return err
			}
			console.Successf("Language switched to %s", lang)
			return nil
		},
	}
	return cmd
}

func configInit(runtime *app.Runtime, force bool) error {
	console := runtime.Console
	_ = force
	items, err := config.ListSiteCatalog()
	if err != nil {
		return err
	}
	console.Successf("Initialized site catalog database: %s", config.SiteCatalogPath())
	console.Infof("Managed sites: %d", len(items))
	return nil
}

func configPtr() *config.Config {
	cfg := config.DefaultConfig()
	return &cfg
}
