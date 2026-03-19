package config

import (
	_ "embed"
	"os"
	"path/filepath"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const (
	DataDir               = "data"
	DefaultConfigFilename = "data/settings.toml"
)

//go:embed resources/settings.sample.toml
var sampleConfig string

func SampleConfig() string {
	return sampleConfig
}

func WriteDefault(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return os.ErrExist
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(sampleConfig), 0o644)
}

func DefaultConfig() Config {
	return Config{
		General: GeneralConfig{
			RawDataDir:        "./data/raw_data",
			OutputDir:         "./data/downloads",
			CacheDir:          "./data/novel_cache",
			RequestInterval:   0.5,
			Workers:           4,
			MaxConnections:    10,
			MaxRPS:            5.0,
			RetryTimes:        3,
			BackoffFactor:     2.0,
			Timeout:           10.0,
			StorageBatchSize:  1,
			CacheBookInfo:     true,
			CacheChapter:      true,
			FetchInaccessible: false,
			Backend:           "nethttp",
			LocaleStyle:       "simplified",
			LoginRequired:     false,
			Output: OutputConfig{
				Formats:              []string{"txt", "epub"},
				AppendTimestamp:      true,
				RenderMissingChapter: true,
				FilenameTemplate:     "{title}_{author}",
				IncludePicture:       true,
			},
			Parser: ParserConfig{
				EnableOCR:       false,
				BatchSize:       32,
				RemoveWatermark: false,
				ModelName:       "PP-OCRv5_mobile_rec",
			},
			Debug: DebugConfig{
				SaveHTML: false,
				LogDir:   "./data/logs",
				LogLevel: "INFO",
			},
			Processors: []ProcessorConfig{{
				Name:            "cleaner",
				Overwrite:       false,
				RemoveInvisible: true,
			}},
		},
		Sites: map[string]SiteConfig{
			"esjzone": {
				BookIDs:       []model.BookRef{{BookID: "1660702902"}},
				LoginRequired: boolPtr(true),
				MirrorHosts:   []string{"https://www.esjzone.me", "https://esjzone.me"},
			},
			"westnovel": {
				BookIDs: []model.BookRef{{BookID: "wuxia-ynyh"}},
			},
			"yibige": {
				BookIDs: []model.BookRef{{BookID: "6238"}},
			},
			"yodu": {
				BookIDs: []model.BookRef{{BookID: "1"}},
			},
			"linovelib": {
				BookIDs: []model.BookRef{{BookID: "8"}},
			},
			"n23qb": {
				BookIDs: []model.BookRef{{BookID: "12282"}},
			},
			"biquge345": {
				BookIDs: []model.BookRef{{BookID: "151120"}},
			},
			"biquge5": {
				BookIDs: []model.BookRef{{BookID: "9_9194"}},
			},
			"fsshu": {
				BookIDs: []model.BookRef{{BookID: "100_100256"}},
			},
			"n69shuba": {
				BookIDs: []model.BookRef{{BookID: "54065"}},
			},
			"piaotia": {
				BookIDs: []model.BookRef{{BookID: "1-1705"}},
			},
			"ixdzs8": {
				BookIDs: []model.BookRef{{BookID: "15918"}},
			},
		},
		Plugins: PluginsConfig{
			EnableLocalPlugins: false,
			OverrideBuiltins:   false,
			LocalPluginsPath:   "./novel_plugins",
		},
	}
}

func boolPtr(v bool) *bool {
	return &v
}
