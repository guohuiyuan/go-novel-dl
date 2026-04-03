package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const (
	DataDir               = "data"
	DefaultConfigFilename = "data/site_catalog.db"
)

func WriteDefault(path string, force bool) error {
	_ = force
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigFilename
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return nil
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
			WebPageSize:       50,
			CLIPageSize:       30,
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
				MirrorHosts:   []string{"https://www.esjzone.one"},
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
			"novalpie": {
				BookIDs:       []model.BookRef{{BookID: "353245"}},
				LoginRequired: boolPtr(true),
			},
			"ruochu": {
				BookIDs: []model.BookRef{{BookID: "121261"}},
			},
			"n17k": {
				BookIDs: []model.BookRef{{BookID: "3374595"}},
			},
			"hongxiuzhao": {
				BookIDs: []model.BookRef{{BookID: "ZG6rmWO"}},
			},
			"fanqienovel": {
				BookIDs: []model.BookRef{{BookID: "7276384138653862966"}},
			},
			"faloo": {
				BookIDs: []model.BookRef{{BookID: "1482723"}},
			},
			"wenku8": {
				BookIDs: []model.BookRef{{BookID: "2835"}},
			},
			"sfacg": {
				BookIDs: []model.BookRef{{BookID: "456123"}},
			},
			"ciyuanji": {
				BookIDs: []model.BookRef{{BookID: "12030"}},
			},
			"qbtr": {
				BookIDs: []model.BookRef{{BookID: "tongren-8978"}},
			},
			"ciweimao": {
				BookIDs: []model.BookRef{{BookID: "100011781"}},
			},
			"n8novel": {
				BookIDs:     []model.BookRef{{BookID: "3365"}},
				LocaleStyle: "simplified",
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
