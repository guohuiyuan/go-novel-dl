package config

import (
	"sort"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type Config struct {
	General GeneralConfig
	Sites   map[string]SiteConfig
	Plugins PluginsConfig
}

type GeneralConfig struct {
	RawDataDir        string
	OutputDir         string
	CacheDir          string
	RequestInterval   float64
	Workers           int
	MaxConnections    int
	MaxRPS            float64
	RetryTimes        int
	BackoffFactor     float64
	Timeout           float64
	StorageBatchSize  int
	CacheBookInfo     bool
	CacheChapter      bool
	FetchInaccessible bool
	Backend           string
	LocaleStyle       string
	LoginRequired     bool
	WebPageSize       int
	CLIPageSize       int
	Output            OutputConfig
	Parser            ParserConfig
	Debug             DebugConfig
	Processors        []ProcessorConfig
}

type OutputConfig struct {
	Formats              []string
	AppendTimestamp      bool
	RenderMissingChapter bool
	FilenameTemplate     string
	IncludePicture       bool
}

type OutputOverride struct {
	Formats              []string
	AppendTimestamp      *bool
	RenderMissingChapter *bool
	FilenameTemplate     *string
	IncludePicture       *bool
}

type ParserConfig struct {
	EnableOCR       bool
	BatchSize       int
	RemoveWatermark bool
	ModelName       string
}

type ParserOverride struct {
	EnableOCR       *bool
	BatchSize       *int
	RemoveWatermark *bool
	ModelName       *string
}

type DebugConfig struct {
	SaveHTML bool
	LogDir   string
	LogLevel string
}

type DebugOverride struct {
	SaveHTML *bool
	LogDir   *string
	LogLevel *string
}

type ProcessorConfig struct {
	Name            string
	Overwrite       bool
	RemoveInvisible bool
}

type PluginsConfig struct {
	EnableLocalPlugins bool
	OverrideBuiltins   bool
	LocalPluginsPath   string
}

type SiteConfig struct {
	BookIDs           []model.BookRef
	LoginRequired     *bool
	Username          string
	Password          string
	Email             string
	Cookie            string
	MirrorHosts       []string
	RequestInterval   *float64
	Workers           *int
	MaxConnections    *int
	MaxRPS            *float64
	RetryTimes        *int
	BackoffFactor     *float64
	Timeout           *float64
	StorageBatchSize  *int
	CacheBookInfo     *bool
	CacheChapter      *bool
	FetchInaccessible *bool
	Backend           string
	LocaleStyle       string
	Output            *OutputOverride
	Parser            *ParserOverride
	Debug             *DebugOverride
}

type ResolvedSiteConfig struct {
	Key         string
	General     GeneralConfig
	BookIDs     []model.BookRef
	Username    string
	Password    string
	Email       string
	Cookie      string
	MirrorHosts []string
}

func (c Config) SiteKeys() []string {
	keys := make([]string, 0, len(c.Sites))
	for key := range c.Sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (c Config) ResolveSiteConfig(site string) ResolvedSiteConfig {
	resolved := ResolvedSiteConfig{
		Key:     site,
		General: c.General,
	}
	resolved.General.Output.Formats = cloneStrings(c.General.Output.Formats)
	resolved.General.Processors = cloneProcessors(c.General.Processors)

	siteCfg, ok := c.Sites[site]
	if !ok {
		return resolved
	}

	resolved.BookIDs = cloneBookRefs(siteCfg.BookIDs)
	resolved.Username = siteCfg.Username
	resolved.Password = siteCfg.Password
	resolved.Email = siteCfg.Email
	resolved.Cookie = siteCfg.Cookie
	resolved.MirrorHosts = cloneStrings(siteCfg.MirrorHosts)

	if siteCfg.LoginRequired != nil {
		resolved.General.LoginRequired = *siteCfg.LoginRequired
	}
	if siteCfg.RequestInterval != nil {
		resolved.General.RequestInterval = *siteCfg.RequestInterval
	}
	if siteCfg.Workers != nil {
		resolved.General.Workers = *siteCfg.Workers
	}
	if siteCfg.MaxConnections != nil {
		resolved.General.MaxConnections = *siteCfg.MaxConnections
	}
	if siteCfg.MaxRPS != nil {
		resolved.General.MaxRPS = *siteCfg.MaxRPS
	}
	if siteCfg.RetryTimes != nil {
		resolved.General.RetryTimes = *siteCfg.RetryTimes
	}
	if siteCfg.BackoffFactor != nil {
		resolved.General.BackoffFactor = *siteCfg.BackoffFactor
	}
	if siteCfg.Timeout != nil {
		resolved.General.Timeout = *siteCfg.Timeout
	}
	if siteCfg.StorageBatchSize != nil {
		resolved.General.StorageBatchSize = *siteCfg.StorageBatchSize
	}
	if siteCfg.CacheBookInfo != nil {
		resolved.General.CacheBookInfo = *siteCfg.CacheBookInfo
	}
	if siteCfg.CacheChapter != nil {
		resolved.General.CacheChapter = *siteCfg.CacheChapter
	}
	if siteCfg.FetchInaccessible != nil {
		resolved.General.FetchInaccessible = *siteCfg.FetchInaccessible
	}
	if siteCfg.Backend != "" {
		resolved.General.Backend = siteCfg.Backend
	}
	if siteCfg.LocaleStyle != "" {
		resolved.General.LocaleStyle = siteCfg.LocaleStyle
	}
	if siteCfg.Output != nil {
		mergeOutputConfig(&resolved.General.Output, siteCfg.Output)
	}
	if siteCfg.Parser != nil {
		mergeParserConfig(&resolved.General.Parser, siteCfg.Parser)
	}
	if siteCfg.Debug != nil {
		mergeDebugConfig(&resolved.General.Debug, siteCfg.Debug)
	}

	return resolved
}

func mergeOutputConfig(dst *OutputConfig, src *OutputOverride) {
	if len(src.Formats) > 0 {
		dst.Formats = cloneStrings(src.Formats)
	}
	if src.AppendTimestamp != nil {
		dst.AppendTimestamp = *src.AppendTimestamp
	}
	if src.RenderMissingChapter != nil {
		dst.RenderMissingChapter = *src.RenderMissingChapter
	}
	if src.FilenameTemplate != nil {
		dst.FilenameTemplate = *src.FilenameTemplate
	}
	if src.IncludePicture != nil {
		dst.IncludePicture = *src.IncludePicture
	}
}

func mergeParserConfig(dst *ParserConfig, src *ParserOverride) {
	if src.EnableOCR != nil {
		dst.EnableOCR = *src.EnableOCR
	}
	if src.BatchSize != nil {
		dst.BatchSize = *src.BatchSize
	}
	if src.RemoveWatermark != nil {
		dst.RemoveWatermark = *src.RemoveWatermark
	}
	if src.ModelName != nil {
		dst.ModelName = *src.ModelName
	}
}

func mergeDebugConfig(dst *DebugConfig, src *DebugOverride) {
	if src.SaveHTML != nil {
		dst.SaveHTML = *src.SaveHTML
	}
	if src.LogDir != nil {
		dst.LogDir = *src.LogDir
	}
	if src.LogLevel != nil {
		dst.LogLevel = *src.LogLevel
	}
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

func cloneBookRefs(items []model.BookRef) []model.BookRef {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]model.BookRef, len(items))
	copy(cloned, items)
	return cloned
}

func cloneProcessors(items []ProcessorConfig) []ProcessorConfig {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]ProcessorConfig, len(items))
	copy(cloned, items)
	return cloned
}
