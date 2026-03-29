package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/progress"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

func newConsole() *ui.Console {
	return ui.NewConsole(os.Stdin, os.Stdout, os.Stderr)
}

func loadRuntime(console *ui.Console, configPath string) (*app.Runtime, string, error) {
	return loadRuntimeWithProgress(console, configPath, true)
}

func loadRuntimeSilent(configPath string) (*app.Runtime, string, error) {
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	return loadRuntimeWithProgress(console, configPath, false)
}

func loadRuntimeWithProgress(console *ui.Console, configPath string, withProgress bool) (*app.Runtime, string, error) {
	cfg, path, err := app.LoadOrInitConfig(console, configPath)
	if err != nil {
		return nil, "", err
	}
	if cfg == nil {
		return nil, path, nil
	}
	runtime := app.NewRuntime(cfg, console)
	if withProgress {
		runtime.Progress = progress.NewConsoleBar(os.Stdout)
	}
	return runtime, path, nil
}

func parseBookRefs(bookIDs []string, startID, endID string) []model.BookRef {
	if len(bookIDs) == 0 {
		return nil
	}

	refs := make([]model.BookRef, 0, len(bookIDs))
	refs = append(refs, model.BookRef{BookID: bookIDs[0], StartID: startID, EndID: endID})
	for _, bookID := range bookIDs[1:] {
		refs = append(refs, model.BookRef{BookID: bookID})
	}
	return refs
}

func ensureSingleURL(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("expected exactly one URL argument when --site is omitted")
	}
	return args[0], nil
}

func normalizeSites(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func withTimeout(parent context.Context, timeoutSeconds float64) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(timeoutSeconds*float64(time.Second)))
}

func requireSite(cmd *cobra.Command, site string) error {
	if strings.TrimSpace(site) == "" {
		return fmt.Errorf("--site is required for this operation")
	}
	_ = cmd
	return nil
}

func defaultInteractiveSites(runtime *app.Runtime) []string {
	if runtime == nil {
		return nil
	}
	return webCompatibleSites(runtime, runtime.DefaultSearchSites())
}

func allInteractiveSites(runtime *app.Runtime) []string {
	if runtime == nil {
		return nil
	}
	return webCompatibleSites(runtime, runtime.AllSearchSites())
}

func webCompatibleSites(runtime *app.Runtime, keys []string) []string {
	if runtime == nil || runtime.Registry == nil {
		return nil
	}

	descriptors := runtime.Registry.SiteDescriptors(keys)
	filtered := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if !interactiveSiteVisible(descriptor) {
			continue
		}
		filtered = append(filtered, descriptor.Key)
	}
	return filtered
}

func interactiveSiteVisible(descriptor site.SiteDescriptor) bool {
	if !descriptor.Capabilities.Search || !descriptor.Capabilities.Download {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(descriptor.Key)) {
	case "biquge345":
		return false
	default:
		return true
	}
}
