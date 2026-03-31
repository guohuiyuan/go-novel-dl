package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func Load(explicitPath string) (*Config, string, error) {
	_ = explicitPath
	cfg := DefaultConfig()
	if err := mergeGeneralConfig(&cfg); err != nil {
		return nil, "", fmt.Errorf("load db general config: %w", err)
	}
	if err := mergeSiteCatalog(&cfg); err != nil {
		return nil, "", fmt.Errorf("load db site catalog: %w", err)
	}
	return &cfg, SiteCatalogPath(), nil
}

func FindConfigPath(explicitPath string) (string, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return "", err
		}
		return explicitPath, nil
	}

	path := filepath.Clean(DefaultConfigFilename)
	if _, err := os.Stat(path); err != nil {
		return "", err
	}

	return path, nil
}

func parseConfig(raw map[string]any) (*Config, error) {
	result := DefaultConfig()

	if generalRaw := asMap(raw["general"]); generalRaw != nil {
		if err := applyGeneral(&result.General, generalRaw); err != nil {
			return nil, err
		}
	}

	if sitesRaw := asMap(raw["sites"]); sitesRaw != nil {
		parsedSites := make(map[string]SiteConfig, len(sitesRaw))
		for siteKey, value := range sitesRaw {
			siteMap := asMap(value)
			if siteMap == nil {
				continue
			}

			siteConfig, err := parseSite(siteMap)
			if err != nil {
				return nil, fmt.Errorf("parse sites.%s: %w", siteKey, err)
			}
			parsedSites[siteKey] = siteConfig
		}
		result.Sites = parsedSites
		if err := SyncSiteCatalogFromConfig(parsedSites); err != nil {
			return nil, err
		}
	}

	if pluginsRaw := asMap(raw["plugins"]); pluginsRaw != nil {
		applyPlugins(&result.Plugins, pluginsRaw)
	}

	if err := mergeSiteCatalog(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func applyGeneral(dst *GeneralConfig, raw map[string]any) error {
	stringField(raw, "raw_data_dir", &dst.RawDataDir)
	stringField(raw, "output_dir", &dst.OutputDir)
	stringField(raw, "cache_dir", &dst.CacheDir)
	floatField(raw, "request_interval", &dst.RequestInterval)
	intField(raw, "workers", &dst.Workers)
	intField(raw, "max_connections", &dst.MaxConnections)
	floatField(raw, "max_rps", &dst.MaxRPS)
	intField(raw, "retry_times", &dst.RetryTimes)
	floatField(raw, "backoff_factor", &dst.BackoffFactor)
	floatField(raw, "timeout", &dst.Timeout)
	intField(raw, "storage_batch_size", &dst.StorageBatchSize)
	boolField(raw, "cache_book_info", &dst.CacheBookInfo)
	boolField(raw, "cache_chapter", &dst.CacheChapter)
	boolField(raw, "fetch_inaccessible", &dst.FetchInaccessible)
	stringField(raw, "backend", &dst.Backend)
	stringField(raw, "locale_style", &dst.LocaleStyle)
	boolField(raw, "login_required", &dst.LoginRequired)

	if outputRaw := asMap(raw["output"]); outputRaw != nil {
		applyOutputConfig(&dst.Output, outputRaw)
	}
	if parserRaw := asMap(raw["parser"]); parserRaw != nil {
		applyParserConfig(&dst.Parser, parserRaw)
	}
	if debugRaw := asMap(raw["debug"]); debugRaw != nil {
		applyDebugConfig(&dst.Debug, debugRaw)
	}
	if processorsRaw := asSlice(raw["processors"]); processorsRaw != nil {
		processors, err := parseProcessors(processorsRaw)
		if err != nil {
			return err
		}
		dst.Processors = processors
	}

	return nil
}

func parseSite(raw map[string]any) (SiteConfig, error) {
	var site SiteConfig

	if bookIDs, ok := raw["book_ids"]; ok {
		parsed, err := parseBookRefs(bookIDs)
		if err != nil {
			return SiteConfig{}, err
		}
		site.BookIDs = parsed
	}

	optionalBoolField(raw, "login_required", &site.LoginRequired)
	stringField(raw, "username", &site.Username)
	stringField(raw, "email", &site.Email)
	stringField(raw, "password", &site.Password)
	stringField(raw, "cookie", &site.Cookie)
	site.MirrorHosts = stringSliceValue(raw["mirror_hosts"])
	optionalFloatField(raw, "request_interval", &site.RequestInterval)
	optionalIntField(raw, "workers", &site.Workers)
	optionalIntField(raw, "max_connections", &site.MaxConnections)
	optionalFloatField(raw, "max_rps", &site.MaxRPS)
	optionalIntField(raw, "retry_times", &site.RetryTimes)
	optionalFloatField(raw, "backoff_factor", &site.BackoffFactor)
	optionalFloatField(raw, "timeout", &site.Timeout)
	optionalIntField(raw, "storage_batch_size", &site.StorageBatchSize)
	optionalBoolField(raw, "cache_book_info", &site.CacheBookInfo)
	optionalBoolField(raw, "cache_chapter", &site.CacheChapter)
	optionalBoolField(raw, "fetch_inaccessible", &site.FetchInaccessible)
	stringField(raw, "backend", &site.Backend)
	stringField(raw, "locale_style", &site.LocaleStyle)

	if outputRaw := asMap(raw["output"]); outputRaw != nil {
		override := OutputOverride{}
		applyOutputOverride(&override, outputRaw)
		site.Output = &override
	}
	if parserRaw := asMap(raw["parser"]); parserRaw != nil {
		override := ParserOverride{}
		applyParserOverride(&override, parserRaw)
		site.Parser = &override
	}
	if debugRaw := asMap(raw["debug"]); debugRaw != nil {
		override := DebugOverride{}
		applyDebugOverride(&override, debugRaw)
		site.Debug = &override
	}

	return site, nil
}

func parseProcessors(raw []any) ([]ProcessorConfig, error) {
	processors := make([]ProcessorConfig, 0, len(raw))
	for _, item := range raw {
		itemMap := asMap(item)
		if itemMap == nil {
			continue
		}

		var cfg ProcessorConfig
		stringField(itemMap, "name", &cfg.Name)
		boolField(itemMap, "overwrite", &cfg.Overwrite)
		boolField(itemMap, "remove_invisible", &cfg.RemoveInvisible)
		if cfg.Name == "" {
			return nil, fmt.Errorf("processor name is required")
		}
		processors = append(processors, cfg)
	}

	return processors, nil
}

func parseBookRefs(raw any) ([]model.BookRef, error) {
	items := asSlice(raw)
	refs := make([]model.BookRef, 0, len(items))
	for _, item := range items {
		switch value := item.(type) {
		case string:
			refs = append(refs, model.BookRef{BookID: value})
		default:
			itemMap := asMap(value)
			if itemMap == nil {
				return nil, fmt.Errorf("unsupported book_ids item %T", item)
			}

			ref := model.BookRef{}
			stringField(itemMap, "book_id", &ref.BookID)
			stringField(itemMap, "start_id", &ref.StartID)
			stringField(itemMap, "end_id", &ref.EndID)
			ref.IgnoreIDs = stringSliceValue(itemMap["ignore_ids"])
			if ref.BookID == "" {
				return nil, fmt.Errorf("book_ids entry missing book_id")
			}
			refs = append(refs, ref)
		}
	}

	return refs, nil
}

func applyOutputConfig(dst *OutputConfig, raw map[string]any) {
	if formats := stringSliceValue(raw["formats"]); len(formats) > 0 {
		dst.Formats = formats
	}
	boolField(raw, "append_timestamp", &dst.AppendTimestamp)
	boolField(raw, "render_missing_chapter", &dst.RenderMissingChapter)
	stringField(raw, "filename_template", &dst.FilenameTemplate)
	boolField(raw, "include_picture", &dst.IncludePicture)
}

func applyOutputOverride(dst *OutputOverride, raw map[string]any) {
	if formats := stringSliceValue(raw["formats"]); len(formats) > 0 {
		dst.Formats = formats
	}
	optionalBoolField(raw, "append_timestamp", &dst.AppendTimestamp)
	optionalBoolField(raw, "render_missing_chapter", &dst.RenderMissingChapter)
	optionalStringField(raw, "filename_template", &dst.FilenameTemplate)
	optionalBoolField(raw, "include_picture", &dst.IncludePicture)
}

func applyParserConfig(dst *ParserConfig, raw map[string]any) {
	boolField(raw, "enable_ocr", &dst.EnableOCR)
	intField(raw, "batch_size", &dst.BatchSize)
	boolField(raw, "remove_watermark", &dst.RemoveWatermark)
	stringField(raw, "model_name", &dst.ModelName)
}

func applyParserOverride(dst *ParserOverride, raw map[string]any) {
	optionalBoolField(raw, "enable_ocr", &dst.EnableOCR)
	optionalIntField(raw, "batch_size", &dst.BatchSize)
	optionalBoolField(raw, "remove_watermark", &dst.RemoveWatermark)
	optionalStringField(raw, "model_name", &dst.ModelName)
}

func applyDebugConfig(dst *DebugConfig, raw map[string]any) {
	boolField(raw, "save_html", &dst.SaveHTML)
	stringField(raw, "log_dir", &dst.LogDir)
	stringField(raw, "log_level", &dst.LogLevel)
}

func applyDebugOverride(dst *DebugOverride, raw map[string]any) {
	optionalBoolField(raw, "save_html", &dst.SaveHTML)
	optionalStringField(raw, "log_dir", &dst.LogDir)
	optionalStringField(raw, "log_level", &dst.LogLevel)
}

func applyPlugins(dst *PluginsConfig, raw map[string]any) {
	boolField(raw, "enable_local_plugins", &dst.EnableLocalPlugins)
	boolField(raw, "override_builtins", &dst.OverrideBuiltins)
	stringField(raw, "local_plugins_path", &dst.LocalPluginsPath)
}

func asMap(value any) map[string]any {
	if value == nil {
		return nil
	}

	if typed, ok := value.(map[string]any); ok {
		return typed
	}

	return nil
}

func asSlice(value any) []any {
	if value == nil {
		return nil
	}

	if typed, ok := value.([]any); ok {
		return typed
	}

	return nil
}

func stringSliceValue(value any) []string {
	items := asSlice(value)
	if len(items) == 0 {
		return nil
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func stringField(raw map[string]any, key string, dst *string) {
	if value, ok := raw[key]; ok {
		if text, ok := value.(string); ok {
			*dst = text
		}
	}
}

func optionalStringField(raw map[string]any, key string, dst **string) {
	if value, ok := raw[key]; ok {
		if text, ok := value.(string); ok {
			*dst = &text
		}
	}
}

func boolField(raw map[string]any, key string, dst *bool) {
	if value, ok := raw[key]; ok {
		if parsed, ok := parseBool(value); ok {
			*dst = parsed
		}
	}
}

func optionalBoolField(raw map[string]any, key string, dst **bool) {
	if value, ok := raw[key]; ok {
		if parsed, ok := parseBool(value); ok {
			*dst = &parsed
		}
	}
}

func intField(raw map[string]any, key string, dst *int) {
	if value, ok := raw[key]; ok {
		if parsed, ok := parseInt(value); ok {
			*dst = parsed
		}
	}
}

func optionalIntField(raw map[string]any, key string, dst **int) {
	if value, ok := raw[key]; ok {
		if parsed, ok := parseInt(value); ok {
			*dst = &parsed
		}
	}
}

func floatField(raw map[string]any, key string, dst *float64) {
	if value, ok := raw[key]; ok {
		if parsed, ok := parseFloat(value); ok {
			*dst = parsed
		}
	}
}

func optionalFloatField(raw map[string]any, key string, dst **float64) {
	if value, ok := raw[key]; ok {
		if parsed, ok := parseFloat(value); ok {
			*dst = &parsed
		}
	}
}

func parseBool(value any) (bool, bool) {
	parsed, ok := value.(bool)
	return parsed, ok
}

func parseInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int64:
		return int(typed), true
	case int32:
		return int(typed), true
	case int:
		return typed, true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func parseFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int64:
		return float64(typed), true
	case int:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}
