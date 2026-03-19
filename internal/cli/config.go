package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage application configuration and settings",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigSetLangCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize default configuration in the current directory",
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
	target := filepath.Join(".", config.DefaultConfigFilename)
	if _, err := os.Stat(target); err == nil && !force {
		console.Infof("File already exists: %s", config.DefaultConfigFilename)
		confirm, err := console.Confirm("Do you want to overwrite settings.toml?", false)
		if err != nil {
			return err
		}
		if !confirm {
			console.Warnf("Skipped: %s", config.DefaultConfigFilename)
			return nil
		}
	}
	if err := config.WriteDefault(target, true); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	console.Successf("Copied: %s", config.DefaultConfigFilename)
	return nil
}

func configPtr() *config.Config {
	cfg := config.DefaultConfig()
	return &cfg
}
