package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	siteCatalogDBFile = "data/site_catalog.db"
)

var (
	siteCatalogDB   *gorm.DB
	siteCatalogOnce sync.Once
	siteCatalogErr  error
)

type siteCatalogEntry struct {
	Key           string    `gorm:"primaryKey;size:64"`
	DisplayName   string    `gorm:"size:128"`
	MirrorHosts   string    `gorm:"type:text"`
	LoginRequired bool      `gorm:"default:false"`
	Username      string    `gorm:"size:256"`
	Password      string    `gorm:"size:256"`
	Cookie        string    `gorm:"type:text"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

type SiteCatalogRecord struct {
	Key           string    `json:"key"`
	DisplayName   string    `json:"display_name"`
	LoginRequired bool      `json:"login_required"`
	Username      string    `json:"username"`
	Password      string    `json:"password"`
	Cookie        string    `json:"cookie"`
	MirrorHosts   []string  `json:"mirror_hosts"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SiteCatalogUpdate struct {
	DisplayName   *string
	LoginRequired *bool
	Username      *string
	Password      *string
	Cookie        *string
	MirrorHosts   *[]string
}

type SiteParameterSupport struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Implemented bool   `json:"implemented"`
	Notes       string `json:"notes,omitempty"`
}

type defaultSiteCatalogRow struct {
	Key           string
	DisplayName   string
	LoginRequired bool
	MirrorHosts   []string
}

var defaultSiteCatalog = []defaultSiteCatalogRow{
	{Key: "esjzone", DisplayName: "ESJ Zone", LoginRequired: true, MirrorHosts: []string{"https://www.esjzone.one"}},
	{Key: "linovelib", DisplayName: "Linovelib"},
	{Key: "n23qb", DisplayName: "N23QB"},
	{Key: "ruochu", DisplayName: "若初"},
	{Key: "fanqienovel", DisplayName: "番茄小说"},
	{Key: "sfacg", DisplayName: "SFACG"},
	{Key: "ciyuanji", DisplayName: "次元纪"},
	{Key: "ciweimao", DisplayName: "刺猬猫"},
	{Key: "novalpie", DisplayName: "Novalpie", LoginRequired: true},
	{Key: "n17k", DisplayName: "17K"},
}

var supportedSiteKeys = func() map[string]struct{} {
	set := make(map[string]struct{}, len(defaultSiteCatalog))
	for _, item := range defaultSiteCatalog {
		set[item.Key] = struct{}{}
	}
	return set
}()

func siteCatalogPath() string {
	if override := strings.TrimSpace(os.Getenv("NOVEL_DL_SITE_DB")); override != "" {
		return override
	}
	return siteCatalogDBFile
}

func SiteCatalogPath() string {
	return siteCatalogPath()
}

func SiteCatalogSupportedKeys() []string {
	keys := make([]string, 0, len(defaultSiteCatalog))
	for _, row := range defaultSiteCatalog {
		keys = append(keys, row.Key)
	}
	return keys
}

func SiteParameterSupports() []SiteParameterSupport {
	return []SiteParameterSupport{
		{Key: "username", Label: "用户名", Implemented: true, Notes: "登录型站点使用"},
		{Key: "password", Label: "密码", Implemented: true, Notes: "登录型站点使用"},
		{Key: "cookie", Label: "Cookie", Implemented: true, Notes: "可用于免登录访问"},
		{Key: "login_required", Label: "登录必需", Implemented: true, Notes: "控制是否执行登录流程"},
		{Key: "mirror_hosts", Label: "镜像地址", Implemented: true, Notes: "用于站点镜像回退"},
		{Key: "book_ids", Label: "Book IDs", Implemented: false, Notes: "不纳入 site_catalog.db，由命令参数或 settings.toml 管理"},
	}
}

func ensureSiteCatalogDB() error {
	siteCatalogOnce.Do(func() {
		path := filepath.Clean(siteCatalogPath())
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			siteCatalogErr = err
			return
		}
		db, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"), &gorm.Config{})
		if err != nil {
			siteCatalogErr = err
			return
		}
		if err := db.AutoMigrate(&siteCatalogEntry{}); err != nil {
			siteCatalogErr = err
			return
		}
		siteCatalogDB = db
		siteCatalogErr = seedSiteCatalog(db)
	})
	return siteCatalogErr
}

func seedSiteCatalog(db *gorm.DB) error {
	if len(defaultSiteCatalog) == 0 {
		return nil
	}
	records := make([]siteCatalogEntry, 0, len(defaultSiteCatalog))
	for _, item := range defaultSiteCatalog {
		mirrored := ""
		if len(item.MirrorHosts) > 0 {
			payload, err := json.Marshal(item.MirrorHosts)
			if err != nil {
				return err
			}
			mirrored = string(payload)
		}
		records = append(records, siteCatalogEntry{
			Key:           item.Key,
			DisplayName:   item.DisplayName,
			MirrorHosts:   mirrored,
			LoginRequired: item.LoginRequired,
		})
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "login_required", "mirror_hosts", "updated_at"}),
	}).Create(&records).Error
}

func loadSiteCatalogEntries() ([]siteCatalogEntry, error) {
	if err := ensureSiteCatalogDB(); err != nil {
		return nil, err
	}
	if siteCatalogDB == nil {
		return nil, fmt.Errorf("site catalog database unavailable")
	}
	var entries []siteCatalogEntry
	if err := siteCatalogDB.Order("key ASC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

func ListSiteCatalog() ([]SiteCatalogRecord, error) {
	entries, err := loadSiteCatalogEntries()
	if err != nil {
		return nil, err
	}
	items := make([]SiteCatalogRecord, 0, len(entries))
	for _, entry := range entries {
		items = append(items, toSiteCatalogRecord(entry))
	}
	return items, nil
}

func UpsertSiteCatalog(siteKey string, patch SiteCatalogUpdate) (SiteCatalogRecord, error) {
	siteKey = strings.TrimSpace(siteKey)
	if siteKey == "" {
		return SiteCatalogRecord{}, fmt.Errorf("site key is required")
	}
	if _, ok := supportedSiteKeys[siteKey]; !ok {
		return SiteCatalogRecord{}, fmt.Errorf("site %q is not in managed catalog", siteKey)
	}
	if err := ensureSiteCatalogDB(); err != nil {
		return SiteCatalogRecord{}, err
	}

	var current siteCatalogEntry
	if err := siteCatalogDB.Where("key = ?", siteKey).Take(&current).Error; err != nil {
		return SiteCatalogRecord{}, err
	}

	if patch.DisplayName != nil {
		current.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.LoginRequired != nil {
		current.LoginRequired = *patch.LoginRequired
	}
	if patch.Username != nil {
		current.Username = strings.TrimSpace(*patch.Username)
	}
	if patch.Password != nil {
		current.Password = strings.TrimSpace(*patch.Password)
	}
	if patch.Cookie != nil {
		current.Cookie = strings.TrimSpace(*patch.Cookie)
	}
	if patch.MirrorHosts != nil {
		current.MirrorHosts = encodeMirrorHosts(*patch.MirrorHosts)
	}

	if err := siteCatalogDB.Save(&current).Error; err != nil {
		return SiteCatalogRecord{}, err
	}
	return toSiteCatalogRecord(current), nil
}

func SyncSiteCatalogFromConfig(sites map[string]SiteConfig) error {
	if len(sites) == 0 {
		return nil
	}
	if err := ensureSiteCatalogDB(); err != nil {
		return err
	}

	for siteKey, siteCfg := range sites {
		if _, ok := supportedSiteKeys[siteKey]; !ok {
			continue
		}

		var current siteCatalogEntry
		if err := siteCatalogDB.Where("key = ?", siteKey).Take(&current).Error; err != nil {
			return err
		}

		changed := false
		if siteCfg.LoginRequired != nil {
			if current.LoginRequired != *siteCfg.LoginRequired {
				current.LoginRequired = *siteCfg.LoginRequired
				changed = true
			}
		}

		username := strings.TrimSpace(siteCfg.Username)
		if username != "" && current.Username != username {
			current.Username = username
			changed = true
		}

		password := strings.TrimSpace(siteCfg.Password)
		if password != "" && current.Password != password {
			current.Password = password
			changed = true
		}

		cookie := strings.TrimSpace(siteCfg.Cookie)
		if cookie != "" && current.Cookie != cookie {
			current.Cookie = cookie
			changed = true
		}

		if len(siteCfg.MirrorHosts) > 0 {
			encoded := encodeMirrorHosts(siteCfg.MirrorHosts)
			if encoded != current.MirrorHosts {
				current.MirrorHosts = encoded
				changed = true
			}
		}

		if !changed {
			continue
		}
		if err := siteCatalogDB.Save(&current).Error; err != nil {
			return err
		}
	}

	return nil
}

func toSiteCatalogRecord(entry siteCatalogEntry) SiteCatalogRecord {
	return SiteCatalogRecord{
		Key:           entry.Key,
		DisplayName:   entry.DisplayName,
		LoginRequired: entry.LoginRequired,
		Username:      entry.Username,
		Password:      entry.Password,
		Cookie:        entry.Cookie,
		MirrorHosts:   parseMirrorHosts(entry.MirrorHosts),
		UpdatedAt:     entry.UpdatedAt,
	}
}

func encodeMirrorHosts(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		normalized = append(normalized, host)
	}
	if len(normalized) == 0 {
		return ""
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	return string(data)
}

func mergeSiteCatalog(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	entries, err := loadSiteCatalogEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if cfg.Sites == nil {
		cfg.Sites = map[string]SiteConfig{}
	}
	for _, entry := range entries {
		siteCfg := cfg.Sites[entry.Key]
		if entry.LoginRequired {
			siteCfg.LoginRequired = boolPtr(true)
		}
		if entry.Username != "" {
			siteCfg.Username = entry.Username
		}
		if entry.Password != "" {
			siteCfg.Password = entry.Password
		}
		if entry.Cookie != "" {
			siteCfg.Cookie = entry.Cookie
		}
		if hosts := parseMirrorHosts(entry.MirrorHosts); len(hosts) > 0 {
			siteCfg.MirrorHosts = hosts
		}
		cfg.Sites[entry.Key] = siteCfg
	}
	return nil
}

func parseMirrorHosts(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	filtered := make([]string, 0, len(decoded))
	for _, host := range decoded {
		host = strings.TrimSpace(host)
		if host != "" {
			filtered = append(filtered, host)
		}
	}
	return filtered
}
