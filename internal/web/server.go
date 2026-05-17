package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goRuntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/progress"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/textconv"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

//go:embed templates/*
var templateFS embed.FS

const RoutePrefix = "/novel"

const defaultWebPageSize = 50
const defaultWebChapterPageSize = 100
const maxWebChapterPageSize = 500

const (
	webBookDetailCacheTTL     = 10 * time.Minute
	webChapterContentCacheTTL = 30 * time.Minute
)

type Service struct {
	Config         *config.Config
	ConfigPath     string
	GeneralConfig  config.GeneralConfigRecord
	Runtime        *app.Runtime
	DefaultSources []site.SiteDescriptor
	AllSources     []site.SiteDescriptor
	Tasks          *DownloadTaskStore
	PageSize       int
	SiteWarnings   []SiteWarning
	SiteStats      []SiteStat
	SiteConfigs    []config.SiteCatalogRecord
	ParamSupports  []config.SiteParameterSupport
	ContentCache   *webContentCache
	cacheMu        sync.Mutex
}

type webContentCache struct {
	mu           sync.Mutex
	books        map[string]cachedBookDetail
	chapters     map[string]cachedChapterContent
	bookCalls    map[string]*bookDetailCall
	chapterCalls map[string]*chapterContentCall
}

type cachedBookDetail struct {
	book      *model.Book
	expiresAt time.Time
}

type cachedChapterContent struct {
	chapter   model.Chapter
	expiresAt time.Time
}

type bookDetailCall struct {
	done chan struct{}
	book *model.Book
	err  error
}

type chapterContentCall struct {
	done    chan struct{}
	chapter model.Chapter
	err     error
}

type SiteWarning struct {
	SiteKey     string `json:"site_key"`
	Message     string `json:"message"`
	Level       string `json:"level"`
	ActionLabel string `json:"action_label,omitempty"`
	ActionLink  string `json:"action_link,omitempty"`
	Transient   bool   `json:"transient,omitempty"`
}

type SiteStat struct {
	SiteKey string   `json:"site_key"`
	Enabled []string `json:"enabled"`
}

type searchRequest struct {
	Keyword   string   `json:"keyword"`
	Scope     string   `json:"scope"`
	Sites     []string `json:"sites"`
	Limit     int      `json:"limit"`
	SiteLimit int      `json:"site_limit"`
	Page      int      `json:"page"`
	PageSize  int      `json:"page_size"`
}

type downloadRequest struct {
	Site    string   `json:"site"`
	BookID  string   `json:"book_id"`
	Formats []string `json:"formats"`
	Target  string   `json:"target,omitempty"`
}

type bookDetailResponse struct {
	Book        model.Book          `json:"book"`
	ChapterPage chapterPageResponse `json:"chapter_page"`
}

type chapterPageResponse struct {
	Page     int  `json:"page"`
	PageSize int  `json:"page_size"`
	Total    int  `json:"total"`
	HasPrev  bool `json:"has_prev"`
	HasNext  bool `json:"has_next"`
}

type paginatedSearchResponse struct {
	app.HybridSearchResponse
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	Total      int  `json:"total"`
	TotalExact bool `json:"total_exact"`
	HasPrev    bool `json:"has_prev"`
	HasNext    bool `json:"has_next"`
}

func Start(port string, shouldOpenBrowser bool, configPath string, cliPageSize int) error {
	service, err := newService(configPath)
	if err != nil {
		return err
	}

	if cliPageSize > 0 {
		service.PageSize = cliPageSize
	}

	router := newRouter(service)
	url := "http://localhost:" + port + RoutePrefix
	if shouldOpenBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = openBrowser(url)
		}()
	}

	fmt.Printf("Web started at %s\n", url)
	return router.Run(":" + port)
}

func newService(configPath string) (*Service, error) {
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	cfg, _, err := app.LoadOrInitConfig(console, configPath)
	if err != nil {
		return nil, err
	}

	runtime := app.NewRuntime(cfg, console)
	runtime.Progress = progress.NullReporter{}

	allSources := searchableDownloadDescriptors(runtime.Registry.SiteDescriptors(runtime.AllSearchSites()))
	defaultSources := defaultAvailableDescriptors(allSources)

	pageSize := webPageSizeFromConfig(cfg)

	warnings := collectSiteWarnings(runtime)
	stats := collectSiteStats(runtime)
	siteConfigs, _ := config.ListSiteCatalog()
	paramSupports := config.SiteParameterSupports()
	generalConfig, _ := config.LoadGeneralConfig()

	tasks := NewDownloadTaskStore()
	if _, err := tasks.HydrateFromConfig(); err != nil {
		fmt.Printf("warn: download task hydrate failed: %v\n", err)
	}

	return &Service{
		Config:         cfg,
		ConfigPath:     configPath,
		GeneralConfig:  generalConfig,
		Runtime:        runtime,
		DefaultSources: defaultSources,
		AllSources:     allSources,
		Tasks:          tasks,
		PageSize:       pageSize,
		SiteWarnings:   warnings,
		SiteStats:      stats,
		SiteConfigs:    siteConfigs,
		ParamSupports:  paramSupports,
		ContentCache:   newWebContentCache(),
	}, nil
}

func (s *Service) reloadRuntime() error {
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	cfg, _, err := app.LoadOrInitConfig(console, s.ConfigPath)
	if err != nil {
		return err
	}
	runtime := app.NewRuntime(cfg, console)
	runtime.Progress = progress.NullReporter{}

	s.Config = cfg
	if general, err := config.LoadGeneralConfig(); err == nil {
		s.GeneralConfig = general
	}
	s.Runtime = runtime
	s.PageSize = webPageSizeFromConfig(cfg)
	s.SiteWarnings = collectSiteWarnings(runtime)
	s.SiteStats = collectSiteStats(runtime)
	s.SiteConfigs, _ = config.ListSiteCatalog()
	s.ParamSupports = config.SiteParameterSupports()
	s.resetContentCache()
	return nil
}

func newWebContentCache() *webContentCache {
	return &webContentCache{
		books:        make(map[string]cachedBookDetail),
		chapters:     make(map[string]cachedChapterContent),
		bookCalls:    make(map[string]*bookDetailCall),
		chapterCalls: make(map[string]*chapterContentCall),
	}
}

func (s *Service) contentCache() *webContentCache {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.ContentCache == nil {
		s.ContentCache = newWebContentCache()
	}
	return s.ContentCache
}

func (s *Service) resetContentCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.ContentCache = newWebContentCache()
}

func (s *Service) hasESJAuthConfigured() bool {
	if s == nil || s.Config == nil {
		return false
	}
	resolved := s.Config.ResolveSiteConfig("esjzone")
	hasCookie := strings.TrimSpace(resolved.Cookie) != ""
	hasCredentials := strings.TrimSpace(resolved.Username) != "" && strings.TrimSpace(resolved.Password) != ""
	return hasCookie || hasCredentials
}

func collectSiteWarnings(runtime *app.Runtime) []SiteWarning {
	if runtime == nil || runtime.Registry == nil {
		return nil
	}
	available := descriptorKeySet(searchableDownloadDescriptors(runtime.Registry.AllSiteDescriptors()))
	warnings := make([]SiteWarning, 0, 2)
	if _, ok := available["esjzone"]; ok {
		warnings = append(warnings, SiteWarning{
			SiteKey:     "esjzone",
			Message:     "临时提示：ESJ Zone 标签搜索偶发超时；遇到 context deadline exceeded 时可先取消该渠道、稍后重试，或在站点配置里添加可用镜像。",
			Level:       "info",
			ActionLabel: "配置站点",
			ActionLink:  "#site-config",
			Transient:   true,
		})
	}
	if _, ok := available["n8novel"]; ok {
		warnings = append(warnings, SiteWarning{
			SiteKey:   "n8novel",
			Message:   "临时提示：无限轻小说近期可能返回 403，已在搜索结果里按临时失败提示处理；可稍后重试或暂时取消该渠道。",
			Level:     "info",
			Transient: true,
		})
	}
	return warnings
}

func collectSiteStats(runtime *app.Runtime) []SiteStat {
	if runtime == nil || runtime.Config == nil || runtime.Registry == nil {
		return nil
	}
	descriptors := runtime.Registry.AllSiteDescriptors()
	stats := make([]SiteStat, 0, len(descriptors))
	for _, descriptor := range descriptors {
		resolved := runtime.Config.ResolveSiteConfig(descriptor.Key)
		fields := make([]string, 0, 6)
		if strings.TrimSpace(resolved.Username) != "" {
			fields = append(fields, "用户名")
		}
		if strings.TrimSpace(resolved.Password) != "" {
			fields = append(fields, "密码")
		}
		if strings.TrimSpace(resolved.Cookie) != "" {
			fields = append(fields, "Cookie")
		}
		if len(resolved.MirrorHosts) > 0 {
			fields = append(fields, "镜像")
		}
		if resolved.General.Workers > 0 {
			fields = append(fields, fmt.Sprintf("协程=%d", resolved.General.Workers))
		}
		if !resolved.General.Output.IncludePicture {
			fields = append(fields, "不抓图")
		}
		if len(fields) > 0 {
			stats = append(stats, SiteStat{
				SiteKey: descriptor.Key,
				Enabled: fields,
			})
		}
	}
	return stats
}

func newRouter(service *Service) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.SetTrustedProxies(nil)

	tmpl := template.Must(template.New("").Funcs(template.FuncMap{
		"toJSON": func(value any) template.JS {
			data, err := json.Marshal(value)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(data)
		},
	}).ParseFS(templateFS, "templates/index.html"))
	router.SetHTMLTemplate(tmpl)

	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, RoutePrefix)
	})

	group := router.Group(RoutePrefix)
	group.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"Root":           RoutePrefix,
			"DefaultSources": service.DefaultSources,
			"AllSources":     service.AllSources,
			"PageSize":       service.PageSize,
			"SiteWarnings":   service.SiteWarnings,
			"SiteStats":      service.SiteStats,
			"GeneralConfig":  service.GeneralConfig,
		})
	})
	group.GET("/style.css", func(c *gin.Context) {
		c.FileFromFS("templates/style.css", http.FS(templateFS))
	})
	group.GET("/icon-256.png", func(c *gin.Context) {
		c.FileFromFS("templates/icon-256.png", http.FS(templateFS))
	})
	group.GET("/app.js", func(c *gin.Context) {
		c.FileFromFS("templates/app.js", http.FS(templateFS))
	})
	group.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	group.GET("/api/meta", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"default_sources": service.DefaultSources,
			"all_sources":     service.AllSources,
			"site_warnings":   service.SiteWarnings,
			"site_stats":      service.SiteStats,
			"general_config":  service.GeneralConfig,
		})
	})
	group.GET("/api/general-config", func(c *gin.Context) {
		record, err := config.LoadGeneralConfig()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"item": record})
	})
	group.PUT("/api/general-config", func(c *gin.Context) {
		var req config.GeneralConfigRecord
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		record, err := config.SaveGeneralConfig(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := service.reloadRuntime(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"item": record})
	})
	group.GET("/api/site-configs", func(c *gin.Context) {
		configs, err := config.ListSiteCatalog()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"items":            configs,
			"param_supports":   config.SiteParameterSupports(),
			"managed_site_key": config.SiteCatalogSupportedKeys(),
		})
	})
	group.PUT("/api/site-configs/:site", func(c *gin.Context) {
		siteKey := strings.TrimSpace(c.Param("site"))
		if siteKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site is required"})
			return
		}

		var req struct {
			LoginRequired *bool    `json:"login_required"`
			WorkerLimit   *int     `json:"worker_limit"`
			FetchImages   *bool    `json:"fetch_images"`
			LocaleStyle   *string  `json:"locale_style"`
			Username      *string  `json:"username"`
			Password      *string  `json:"password"`
			Cookie        *string  `json:"cookie"`
			MirrorHosts   []string `json:"mirror_hosts"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}

		update := config.SiteCatalogUpdate{
			LoginRequired: req.LoginRequired,
			WorkerLimit:   req.WorkerLimit,
			FetchImages:   req.FetchImages,
			LocaleStyle:   req.LocaleStyle,
			Username:      req.Username,
			Password:      req.Password,
			Cookie:        req.Cookie,
		}
		if req.MirrorHosts != nil {
			mirrors := req.MirrorHosts
			update.MirrorHosts = &mirrors
		}

		item, err := config.UpsertSiteCatalog(siteKey, update)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := service.reloadRuntime(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"item":          item,
			"site_warnings": service.SiteWarnings,
			"site_stats":    service.SiteStats,
		})
	})
	group.GET("/api/books/detail", func(c *gin.Context) {
		siteKey := strings.TrimSpace(c.Query("site"))
		bookID := strings.TrimSpace(c.Query("book_id"))
		if siteKey == "" || bookID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site and book_id are required"})
			return
		}

		if !siteSupportsDownload(service.Runtime.Registry, siteKey) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "selected site must support download"})
			return
		}

		if queryBool(c.Query("local")) {
			book, err := service.localBookDetail(siteKey, bookID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "book has not been downloaded to server local storage"})
				return
			}
			chapterPage := queryPositiveInt(c, "chapter_page", 1)
			chapterPageSize := queryPositiveInt(c, "chapter_page_size", defaultWebChapterPageSize)
			c.JSON(http.StatusOK, paginateBookDetail(book, chapterPage, chapterPageSize))
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), detailTimeoutForSite(siteKey))
		defer cancel()

		book, err := service.bookDetail(ctx, siteKey, bookID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		chapterPage := queryPositiveInt(c, "chapter_page", 1)
		chapterPageSize := queryPositiveInt(c, "chapter_page_size", defaultWebChapterPageSize)
		c.JSON(http.StatusOK, paginateBookDetail(book, chapterPage, chapterPageSize))
	})
	group.POST("/api/search", func(c *gin.Context) {
		var req searchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		req.Keyword = strings.TrimSpace(req.Keyword)
		if req.Keyword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "keyword is required"})
			return
		}

		if looksLikeWebURL(req.Keyword) {
			response, err := service.resolveURLSearch(c.Request.Context(), req.Keyword)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, response)
			return
		}

		sites := normalizeSites(req.Sites)
		allowedSites := descriptorKeySet(service.AllSources)
		if len(sites) > 0 {
			sites = filterAllowedSites(sites, allowedSites)
			if len(sites) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "selected sites must support both search and download"})
				return
			}
		}
		if len(sites) == 0 {
			if strings.EqualFold(strings.TrimSpace(req.Scope), "all") {
				sites = descriptorKeys(service.AllSources)
			} else {
				sites = descriptorKeys(service.DefaultSources)
			}
		}
		if len(sites) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no searchable download sources available"})
			return
		}
		page := clampPositive(req.Page, 1)
		pageSize := clampPositive(req.PageSize, service.PageSize)
		fetchLimit := page*pageSize + 1
		siteLimit := req.SiteLimit
		if siteLimit < fetchLimit {
			siteLimit = fetchLimit
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), searchTimeoutForSites(sites))
		defer cancel()

		response, err := service.Runtime.HybridSearch(ctx, req.Keyword, app.HybridSearchOptions{
			Sites:        sites,
			OverallLimit: maxInt(req.Limit, fetchLimit),
			PerSiteLimit: siteLimit,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, paginateSearchResponse(response, page, pageSize))
	})
	group.POST("/api/sources/speedtest", func(c *gin.Context) {
		var req sourceSpeedRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		response := service.runSourceSpeedTest(c.Request.Context(), req)
		c.JSON(http.StatusOK, response)
	})
	group.POST("/api/download-tasks", func(c *gin.Context) {
		var req downloadRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		req.Site = strings.TrimSpace(req.Site)
		req.BookID = strings.TrimSpace(req.BookID)
		if req.Site == "" || req.BookID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site and book_id are required"})
			return
		}

		target := normalizeWebTaskTarget(req.Target)
		req.Target = target
		formats := append([]string(nil), req.Formats...)
		if target == DownloadTaskTargetLocal || target == DownloadTaskTargetShelf {
			formats = nil
		}

		task := service.Tasks.Create(req.Site, req.BookID, DownloadTaskOptions{
			Target:  target,
			Formats: formats,
		})
		if target == DownloadTaskTargetExport || target == DownloadTaskTargetBrowser {
			service.startExportTask(task.ID, req)
		} else {
			service.startDownloadTask(task.ID, req)
		}

		c.JSON(http.StatusAccepted, gin.H{
			"task": task,
		})
	})
	group.GET("/api/download-tasks", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"tasks": service.Tasks.List()})
	})
	group.GET("/api/download-tasks/:id", func(c *gin.Context) {
		task, ok := service.Tasks.Snapshot(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"task": task})
	})
	group.DELETE("/api/download-tasks/:id", func(c *gin.Context) {
		id := strings.TrimSpace(c.Param("id"))
		if !service.Tasks.Delete(id) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": id})
	})
	group.GET("/api/download-file", func(c *gin.Context) {
		filePath := strings.TrimSpace(c.Query("path"))
		if filePath == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
			return
		}

		absPath, err := filepath.Abs(filePath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
			return
		}

		if _, err := os.Stat(absPath); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}

		fileName := filepath.Base(absPath)
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
		c.File(absPath)
	})
	group.GET("/api/chapter-content", func(c *gin.Context) {
		siteKey := strings.TrimSpace(c.Query("site"))
		bookID := strings.TrimSpace(c.Query("book_id"))
		chapterID := strings.TrimSpace(c.Query("chapter_id"))
		chapterTitle := strings.TrimSpace(c.Query("title"))
		chapterURL := strings.TrimSpace(c.Query("url"))
		if siteKey == "" || bookID == "" || (chapterID == "" && chapterTitle == "" && chapterURL == "") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site, book_id, and chapter identity are required"})
			return
		}

		timeout := chapterContentTimeoutForSite(siteKey)
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		ch := model.Chapter{ID: chapterID, Title: chapterTitle, URL: chapterURL}
		if queryBool(c.Query("local")) {
			result, ok := service.lookupChapterInLibrary(siteKey, bookID, ch)
			if !ok {
				c.JSON(http.StatusNotFound, gin.H{"error": "chapter has not been downloaded to server local storage"})
				return
			}
			resolved := service.Config.ResolveSiteConfig(siteKey)
			c.JSON(http.StatusOK, gin.H{"chapter": normalizeChapterForWeb(result, resolved.General.LocaleStyle)})
			return
		}

		result, err := service.chapterContent(ctx, siteKey, bookID, ch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"chapter": result})
	})

	registerBookshelfRoutes(group, service)

	return router
}

// parseBookshelfParentID extracts an optional parent_id from the request. The
// returned ok flag is true when the parameter was syntactically valid (or
// missing); false signals a 400 should be returned.
func parseBookshelfParentID(raw string) (*uint, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" || strings.EqualFold(raw, "null") || strings.EqualFold(raw, "root") {
		return nil, true
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return nil, false
	}
	parent := uint(value)
	return &parent, true
}

func parseBookshelfID(raw string) (uint, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || value == 0 {
		return 0, false
	}
	return uint(value), true
}

func normalizeWebTaskTarget(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case DownloadTaskTargetExport, DownloadTaskTargetBrowser:
		return DownloadTaskTargetExport
	default:
		return DownloadTaskTargetLocal
	}
}

func registerBookshelfRoutes(group *gin.RouterGroup, service *Service) {
	group.GET("/api/bookshelf/items", func(c *gin.Context) {
		parentID, ok := parseBookshelfParentID(c.Query("parent_id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent_id"})
			return
		}
		items, err := config.ListBookshelfItems(parentID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var breadcrumb []config.BookshelfItem
		if parentID != nil {
			breadcrumb, err = config.BookshelfBreadcrumb(*parentID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"items":      items,
			"breadcrumb": breadcrumb,
		})
	})

	group.POST("/api/bookshelf/folders", func(c *gin.Context) {
		var body struct {
			ParentID *uint  `json:"parent_id"`
			Name     string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		item, err := config.CreateBookshelfFolder(body.ParentID, body.Name)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"item": item})
	})

	group.POST("/api/bookshelf/books", func(c *gin.Context) {
		var body struct {
			ParentID      *uint  `json:"parent_id"`
			Site          string `json:"site"`
			BookID        string `json:"book_id"`
			Title         string `json:"title"`
			Author        string `json:"author"`
			CoverURL      string `json:"cover_url"`
			Description   string `json:"description"`
			LatestChapter string `json:"latest_chapter"`
			SourceURL     string `json:"source_url"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		item, err := config.AddBookshelfBook(config.BookshelfBookInput{
			ParentID:      body.ParentID,
			Site:          body.Site,
			BookID:        body.BookID,
			Title:         body.Title,
			Author:        body.Author,
			CoverURL:      body.CoverURL,
			Description:   body.Description,
			LatestChapter: body.LatestChapter,
			SourceURL:     body.SourceURL,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"item": item})
	})

	group.PATCH("/api/bookshelf/items/:id", func(c *gin.Context) {
		id, ok := parseBookshelfID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}
		var body struct {
			Name        *string `json:"name,omitempty"`
			ParentID    *uint   `json:"parent_id,omitempty"`
			ClearParent bool    `json:"clear_parent,omitempty"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		patch := config.BookshelfMutation{
			Name:        body.Name,
			ParentID:    body.ParentID,
			ClearParent: body.ClearParent,
		}
		item, err := config.UpdateBookshelfItem(id, patch)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"item": item})
	})

	group.DELETE("/api/bookshelf/items/:id", func(c *gin.Context) {
		id, ok := parseBookshelfID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}
		if err := config.DeleteBookshelfItem(id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": id})
	})

	group.POST("/api/bookshelf/items/:id/cache", func(c *gin.Context) {
		id, ok := parseBookshelfID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}
		item, err := config.GetBookshelfItem(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "bookshelf item not found"})
			return
		}
		if item.Kind != config.BookshelfItemKindBook {
			c.JSON(http.StatusBadRequest, gin.H{"error": "only book items can be cached"})
			return
		}
		if strings.TrimSpace(item.Site) == "" || strings.TrimSpace(item.BookID) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bookshelf item is missing site/book_id"})
			return
		}
		task := service.Tasks.Create(item.Site, item.BookID, DownloadTaskOptions{
			Target: DownloadTaskTargetLocal,
		})
		service.startDownloadTask(task.ID, downloadRequest{
			Site:   item.Site,
			BookID: item.BookID,
			Target: DownloadTaskTargetLocal,
		})
		c.JSON(http.StatusAccepted, gin.H{"task": task})
	})

	group.POST("/api/bookshelf/progress", func(c *gin.Context) {
		var body struct {
			Site          string `json:"site"`
			BookID        string `json:"book_id"`
			ChapterID     string `json:"chapter_id"`
			ChapterIndex  int    `json:"chapter_index"`
			ChapterTitle  string `json:"chapter_title"`
			Title         string `json:"title"`
			Author        string `json:"author"`
			CoverURL      string `json:"cover_url"`
			Description   string `json:"description"`
			LatestChapter string `json:"latest_chapter"`
			SourceURL     string `json:"source_url"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		if strings.TrimSpace(body.Site) == "" || strings.TrimSpace(body.BookID) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site and book_id are required"})
			return
		}
		item, _, err := config.UpdateBookshelfProgress(config.BookshelfProgressInput{
			Site:          body.Site,
			BookID:        body.BookID,
			ChapterID:     body.ChapterID,
			ChapterIndex:  body.ChapterIndex,
			ChapterTitle:  body.ChapterTitle,
			Title:         body.Title,
			Author:        body.Author,
			CoverURL:      body.CoverURL,
			Description:   body.Description,
			LatestChapter: body.LatestChapter,
			SourceURL:     body.SourceURL,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"updated": true, "item": item})
	})

	group.GET("/api/bookshelf/history", func(c *gin.Context) {
		limit := 0
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				limit = value
			}
		}
		items, err := config.ListBookshelfHistory(limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
}

func looksLikeWebURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.TrimSpace(parsed.Host) != ""
}

func (s *Service) resolveURLSearch(ctx context.Context, rawURL string) (paginatedSearchResponse, error) {
	resolved, ok := site.ResolveURL(s.Runtime.Registry, rawURL)
	if !ok || resolved == nil || strings.TrimSpace(resolved.SiteKey) == "" || strings.TrimSpace(resolved.BookID) == "" {
		return paginatedSearchResponse{}, fmt.Errorf("无法识别该链接，当前仅支持受支持站点的书籍详情或章节链接")
	}
	if !siteSupportsDownload(s.Runtime.Registry, resolved.SiteKey) {
		return paginatedSearchResponse{}, fmt.Errorf("该链接所属站点当前不支持 Web 下载")
	}
	detailCtx, cancel := context.WithTimeout(ctx, detailTimeoutForSite(resolved.SiteKey))
	defer cancel()
	book, err := s.bookDetail(detailCtx, resolved.SiteKey, resolved.BookID)
	if err != nil {
		return paginatedSearchResponse{}, err
	}

	itemURL := strings.TrimSpace(resolved.Canonical)
	if itemURL == "" {
		itemURL = strings.TrimSpace(book.SourceURL)
	}
	if itemURL == "" {
		itemURL = strings.TrimSpace(rawURL)
	}

	primary := model.SearchResult{
		Site:        resolved.SiteKey,
		BookID:      resolved.BookID,
		Title:       strings.TrimSpace(book.Title),
		Author:      strings.TrimSpace(book.Author),
		Description: strings.TrimSpace(book.Description),
		URL:         itemURL,
		CoverURL:    strings.TrimSpace(book.CoverURL),
	}
	if primary.Title == "" {
		primary.Title = resolved.BookID
	}
	if primary.Author == "" {
		primary.Author = "Unknown"
	}

	response := app.HybridSearchResponse{
		Keyword: rawURL,
		Sites:   []string{resolved.SiteKey},
		Results: []app.HybridSearchResult{{
			Key:           resolved.SiteKey + "|" + resolved.BookID,
			Title:         primary.Title,
			Author:        primary.Author,
			Description:   primary.Description,
			CoverURL:      primary.CoverURL,
			PreferredSite: resolved.SiteKey,
			Primary:       primary,
			Variants:      []model.SearchResult{primary},
			SourceCount:   1,
			Score:         1,
		}},
	}
	return paginateSearchResponse(response, 1, 1), nil
}

func (s *Service) startDownloadTask(taskID string, req downloadRequest) {
	go func() {
		defer func() {
			_ = s.reloadRuntime()
		}()

		s.Tasks.MarkLoadingChapters(taskID, req.Site, req.BookID)

		target := strings.TrimSpace(strings.ToLower(req.Target))
		skipExport := target == DownloadTaskTargetLocal || target == DownloadTaskTargetShelf

		runtime := s.newTaskRuntime(taskID)
		results, err := runtime.Download(context.Background(), req.Site, []model.BookRef{{
			BookID: req.BookID,
		}}, req.Formats, skipExport)
		if err != nil {
			s.Tasks.MarkFailed(taskID, err)
			return
		}
		if len(results) == 0 {
			s.Tasks.MarkFailed(taskID, fmt.Errorf("download returned no result"))
			return
		}

		exported := make([]string, 0)
		title := results[0].Book.Title
		totalChapters := 0
		cachedChapters := 0
		for _, result := range results {
			exported = append(exported, result.Exported...)
			if result.Book == nil {
				continue
			}
			if strings.TrimSpace(title) == "" {
				title = result.Book.Title
			}
			if chCount := len(result.Book.Chapters); chCount > totalChapters {
				totalChapters = chCount
			}
			for _, chapter := range result.Book.Chapters {
				if chapter.Downloaded || strings.TrimSpace(chapter.Content) != "" {
					cachedChapters++
				}
			}
		}
		if totalChapters > 0 {
			if err := config.UpdateBookshelfCacheStats(req.Site, req.BookID, totalChapters, cachedChapters); err != nil {
				fmt.Printf("warn: update bookshelf cache stats failed: %v\n", err)
			}
		}
		s.Tasks.MarkCompleted(taskID, title, exported)
	}()
}

func (s *Service) startExportTask(taskID string, req downloadRequest) {
	go func() {
		defer func() {
			_ = s.reloadRuntime()
		}()

		s.Tasks.MarkLoadingChapters(taskID, req.Site, req.BookID)

		runtime := s.newTaskRuntime(taskID)
		results, err := runtime.Download(context.Background(), req.Site, []model.BookRef{{
			BookID: req.BookID,
		}}, req.Formats, false)
		if err != nil {
			s.Tasks.MarkFailed(taskID, err)
			return
		}
		if len(results) == 0 {
			s.Tasks.MarkFailed(taskID, fmt.Errorf("export returned no result"))
			return
		}

		exported := make([]string, 0)
		title := results[0].Book.Title
		totalChapters := 0
		cachedChapters := 0
		for _, result := range results {
			exported = append(exported, result.Exported...)
			if result.Book == nil {
				continue
			}
			if strings.TrimSpace(title) == "" {
				title = result.Book.Title
			}
			if chCount := len(result.Book.Chapters); chCount > totalChapters {
				totalChapters = chCount
			}
			for _, chapter := range result.Book.Chapters {
				if chapter.Downloaded || strings.TrimSpace(chapter.Content) != "" {
					cachedChapters++
				}
			}
		}
		if totalChapters > 0 {
			if err := config.UpdateBookshelfCacheStats(req.Site, req.BookID, totalChapters, cachedChapters); err != nil {
				fmt.Printf("warn: update bookshelf cache stats failed: %v\n", err)
			}
		}
		s.Tasks.MarkCompleted(taskID, title, exported)
	}()
}

func (s *Service) newTaskRuntime(taskID string) *app.Runtime {
	console := ui.NewConsole(strings.NewReader(""), io.Discard, io.Discard)
	runtime := app.NewRuntime(s.Config, console)
	runtime.Progress = &taskReporter{
		store:  s.Tasks,
		taskID: taskID,
	}
	return runtime
}

func (s *Service) localBookDetail(siteKey, bookID string) (*model.Book, error) {
	if s.Runtime == nil || s.Runtime.Library == nil {
		return nil, fmt.Errorf("local library is not available")
	}
	book, _, err := s.Runtime.Library.LoadBook(siteKey, bookID, "")
	if err != nil {
		return nil, err
	}
	resolved := s.Config.ResolveSiteConfig(siteKey)
	return textconv.NormalizeBookLocale(book, resolved.General.LocaleStyle), nil
}

func (s *Service) bookDetail(ctx context.Context, siteKey, bookID string) (*model.Book, error) {
	cacheKey := detailCacheKey(siteKey, bookID)
	if cached, ok := s.contentCache().getBook(cacheKey); ok {
		return cached, nil
	}
	if book, err, shared := s.contentCache().joinBook(cacheKey); shared {
		return book, err
	}
	book, err := s.fetchBookDetail(ctx, siteKey, bookID)
	s.contentCache().finishBook(cacheKey, book, err, webBookDetailCacheTTL)
	return book, err
}

func (s *Service) fetchBookDetail(ctx context.Context, siteKey, bookID string) (*model.Book, error) {
	resolved := s.Config.ResolveSiteConfig(siteKey)
	client, err := s.Runtime.Registry.Build(siteKey, resolved)
	if err != nil {
		return nil, err
	}

	book, err := client.DownloadPlan(ctx, model.BookRef{BookID: bookID})
	if err != nil {
		return nil, err
	}
	if book == nil {
		return nil, fmt.Errorf("download plan returned no book")
	}
	if strings.TrimSpace(book.Site) == "" {
		book.Site = siteKey
	}
	if strings.TrimSpace(book.ID) == "" {
		book.ID = bookID
	}
	book = textconv.NormalizeBookLocale(book, resolved.General.LocaleStyle)
	return book, nil
}

func (s *Service) chapterContent(ctx context.Context, siteKey, bookID string, chapter model.Chapter) (model.Chapter, error) {
	cacheKey := chapterCacheKey(siteKey, bookID, chapter)
	if cached, ok := s.contentCache().getChapter(cacheKey); ok {
		return cached, nil
	}
	if loaded, err, shared := s.contentCache().joinChapter(cacheKey); shared {
		return loaded, err
	}
	loaded, err := s.fetchChapterContent(ctx, siteKey, bookID, chapter)
	s.contentCache().finishChapter(cacheKey, loaded, err, webChapterContentCacheTTL)
	return loaded, err
}

func (s *Service) fetchChapterContent(ctx context.Context, siteKey, bookID string, chapter model.Chapter) (model.Chapter, error) {
	resolved := s.Config.ResolveSiteConfig(siteKey)
	if cached, ok := s.lookupChapterInLibrary(siteKey, bookID, chapter); ok {
		return normalizeChapterForWeb(cached, resolved.General.LocaleStyle), nil
	}
	client, err := s.Runtime.Registry.Build(siteKey, resolved)
	if err != nil {
		return chapter, err
	}
	loaded, err := client.FetchChapter(ctx, bookID, chapter)
	if err != nil {
		return loaded, err
	}
	return normalizeChapterForWeb(loaded, resolved.General.LocaleStyle), nil
}

// lookupChapterInLibrary returns a chapter previously persisted into the local
// library (e.g. via a shelf-target download). Matching prefers the chapter ID;
// falling back to URL and title keeps it resilient to ID drift across sites.
func (s *Service) lookupChapterInLibrary(siteKey, bookID string, chapter model.Chapter) (model.Chapter, bool) {
	if s.Runtime == nil || s.Runtime.Library == nil {
		return model.Chapter{}, false
	}
	if strings.TrimSpace(siteKey) == "" || strings.TrimSpace(bookID) == "" {
		return model.Chapter{}, false
	}
	state, err := s.Runtime.Library.LoadBookState(siteKey, bookID, "")
	if err != nil || state == nil || state.Book == nil {
		return model.Chapter{}, false
	}
	if chapter.ID != "" {
		if cached, ok := state.ChapterByID[chapter.ID]; ok && strings.TrimSpace(cached.Content) != "" {
			return cached, true
		}
	}
	for _, candidate := range state.Book.Chapters {
		if strings.TrimSpace(candidate.Content) == "" {
			continue
		}
		if chapter.URL != "" && candidate.URL == chapter.URL {
			return candidate, true
		}
		if chapter.ID != "" && candidate.ID == chapter.ID {
			return candidate, true
		}
		if chapter.Title != "" && candidate.Title == chapter.Title {
			return candidate, true
		}
	}
	return model.Chapter{}, false
}

func normalizeChapterForWeb(chapter model.Chapter, localeStyle string) model.Chapter {
	style := strings.ToLower(strings.TrimSpace(localeStyle))
	if style == "" || style == "original" || style == "traditional" {
		return chapter
	}
	if style != "simplified" && style != "zh_cn" && style != "zh-cn" && style != "zh-hans" {
		return chapter
	}
	chapter.Title = textconv.ToSimplified(chapter.Title)
	chapter.Content = textconv.ToSimplified(chapter.Content)
	chapter.Volume = textconv.ToSimplified(chapter.Volume)
	return chapter
}

func (c *webContentCache) getBook(key string) (*model.Book, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.books[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(cached.expiresAt) {
		delete(c.books, key)
		return nil, false
	}
	return cached.book.Clone(), true
}

func (c *webContentCache) joinBook(key string) (*model.Book, error, bool) {
	c.mu.Lock()
	if call, ok := c.bookCalls[key]; ok {
		c.mu.Unlock()
		<-call.done
		if call.book == nil {
			return nil, call.err, true
		}
		return call.book.Clone(), call.err, true
	}
	c.bookCalls[key] = &bookDetailCall{done: make(chan struct{})}
	c.mu.Unlock()
	return nil, nil, false
}

func (c *webContentCache) finishBook(key string, book *model.Book, err error, ttl time.Duration) {
	c.mu.Lock()
	call := c.bookCalls[key]
	if call != nil {
		if book != nil {
			call.book = book.Clone()
		}
		call.err = err
		delete(c.bookCalls, key)
	}
	if err == nil && book != nil && ttl > 0 {
		c.books[key] = cachedBookDetail{
			book:      book.Clone(),
			expiresAt: time.Now().Add(ttl),
		}
	}
	c.mu.Unlock()
	if call != nil {
		close(call.done)
	}
}

func (c *webContentCache) getChapter(key string) (model.Chapter, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.chapters[key]
	if !ok {
		return model.Chapter{}, false
	}
	if time.Now().After(cached.expiresAt) {
		delete(c.chapters, key)
		return model.Chapter{}, false
	}
	return cached.chapter, true
}

func (c *webContentCache) joinChapter(key string) (model.Chapter, error, bool) {
	c.mu.Lock()
	if call, ok := c.chapterCalls[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.chapter, call.err, true
	}
	c.chapterCalls[key] = &chapterContentCall{done: make(chan struct{})}
	c.mu.Unlock()
	return model.Chapter{}, nil, false
}

func (c *webContentCache) finishChapter(key string, chapter model.Chapter, err error, ttl time.Duration) {
	c.mu.Lock()
	call := c.chapterCalls[key]
	if call != nil {
		call.chapter = chapter
		call.err = err
		delete(c.chapterCalls, key)
	}
	if err == nil && strings.TrimSpace(chapter.Content) != "" && ttl > 0 {
		c.chapters[key] = cachedChapterContent{
			chapter:   chapter,
			expiresAt: time.Now().Add(ttl),
		}
	}
	c.mu.Unlock()
	if call != nil {
		close(call.done)
	}
}

func detailCacheKey(siteKey, bookID string) string {
	return strings.TrimSpace(siteKey) + "/" + strings.TrimSpace(bookID)
}

func chapterCacheKey(siteKey, bookID string, chapter model.Chapter) string {
	chapterID := strings.TrimSpace(chapter.ID)
	if chapterID == "" {
		chapterID = strings.TrimSpace(chapter.URL)
	}
	if chapterID == "" {
		chapterID = strings.TrimSpace(chapter.Title)
	}
	return detailCacheKey(siteKey, bookID) + "/" + chapterID
}

type taskReporter struct {
	store    *DownloadTaskStore
	taskID   string
	lastEmit time.Time
	lastDone int
}

func (r *taskReporter) OnBookStart(siteKey, bookID, title string, total int) {
	r.lastEmit = time.Time{}
	r.lastDone = 0
	r.store.MarkRunning(r.taskID, siteKey, bookID, title, total)
}

func (r *taskReporter) OnBookProgress(done, total int, chapterTitle string) {
	now := time.Now().UTC()
	if total > 0 && done < total {
		if !r.lastEmit.IsZero() && now.Sub(r.lastEmit) < 350*time.Millisecond && done-r.lastDone < 3 {
			return
		}
	}
	r.lastEmit = now
	r.lastDone = done
	r.store.MarkProgress(r.taskID, done, total, chapterTitle)
}

func (r *taskReporter) OnBookComplete(done, total int) {
	r.store.MarkExporting(r.taskID, done, total)
}

func openBrowser(url string) error {
	switch goRuntime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func normalizeSites(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(items))
	sites := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		sites = append(sites, item)
	}
	return sites
}

func searchableDownloadDescriptors(items []site.SiteDescriptor) []site.SiteDescriptor {
	if len(items) == 0 {
		return nil
	}

	filtered := make([]site.SiteDescriptor, 0, len(items))
	for _, item := range items {
		if !item.Capabilities.Search || !item.Capabilities.Download {
			continue
		}
		if hideWebSource(item.Key) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

// defaultAvailableDescriptors keeps only the descriptors whose key is in the
// curated default-available list, preserving the input ordering. Used to seed
// the UI's pre-selected channels on first visit.
func defaultAvailableDescriptors(items []site.SiteDescriptor) []site.SiteDescriptor {
	if len(items) == 0 {
		return nil
	}
	allowed := site.DefaultAvailableSiteSet()
	filtered := make([]site.SiteDescriptor, 0, len(allowed))
	for _, item := range items {
		if _, ok := allowed[item.Key]; !ok {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func siteSupportsDownload(registry *site.Registry, siteKey string) bool {
	if registry == nil {
		return false
	}
	descriptor, ok := registry.SiteDescriptor(siteKey)
	return ok && descriptor.Capabilities.Download
}

func descriptorKeys(items []site.SiteDescriptor) []string {
	if len(items) == 0 {
		return nil
	}

	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}

func descriptorKeySet(items []site.SiteDescriptor) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item.Key] = struct{}{}
	}
	return set
}

func filterAllowedSites(items []string, allowed map[string]struct{}) []string {
	if len(items) == 0 || len(allowed) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item]; !ok {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func containsSite(items []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	if target == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(strings.ToLower(item)) == target {
			return true
		}
	}
	return false
}

func paginateSearchResponse(response app.HybridSearchResponse, page, pageSize int) paginatedSearchResponse {
	page = clampPositive(page, 1)
	pageSize = clampPositive(pageSize, defaultWebPageSize)

	offset := (page - 1) * pageSize
	total := len(response.Results)
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	hasNext := total > end
	pageResults := make([]app.HybridSearchResult, 0, end-offset)
	if offset < end {
		pageResults = append(pageResults, response.Results[offset:end]...)
	}
	response.Results = pageResults

	return paginatedSearchResponse{
		HybridSearchResponse: response,
		Page:                 page,
		PageSize:             pageSize,
		Total:                total,
		TotalExact:           !hasNext,
		HasPrev:              offset > 0,
		HasNext:              hasNext,
	}
}

func paginateBookDetail(book *model.Book, page, pageSize int) bookDetailResponse {
	if book == nil {
		return bookDetailResponse{}
	}
	page = clampPositive(page, 1)
	pageSize = clampPositive(pageSize, defaultWebChapterPageSize)
	if pageSize > maxWebChapterPageSize {
		pageSize = maxWebChapterPageSize
	}

	total := len(book.Chapters)
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * pageSize
	if offset > total {
		offset = total
	}
	end := offset + pageSize
	if end > total {
		end = total
	}

	pagedBook := book.Clone()
	if offset < end {
		pagedBook.Chapters = append([]model.Chapter(nil), book.Chapters[offset:end]...)
	} else {
		pagedBook.Chapters = nil
	}
	return bookDetailResponse{
		Book: *pagedBook,
		ChapterPage: chapterPageResponse{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
			HasPrev:  offset > 0,
			HasNext:  total > end,
		},
	}
}

func queryPositiveInt(c *gin.Context, key string, fallback int) int {
	if c == nil {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func queryBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func webPageSizeFromConfig(cfg *config.Config) int {
	if cfg == nil || cfg.General.WebPageSize <= 0 {
		return defaultWebPageSize
	}
	return cfg.General.WebPageSize
}

func clampPositive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func searchTimeoutForSites(sites []string) time.Duration {
	timeout := 12 * time.Second
	for _, site := range sites {
		switch strings.ToLower(strings.TrimSpace(site)) {
		case "esjzone":
			timeout = maxDuration(timeout, 50*time.Second)
		case "n8novel":
			timeout = maxDuration(timeout, 45*time.Second)
		case "tongrenshe":
			timeout = maxDuration(timeout, 45*time.Second)
		case "tianyabooks":
			timeout = maxDuration(timeout, 3*time.Minute)
		case "biquge5", "piaotia", "qbtr":
			timeout = maxDuration(timeout, 45*time.Second)
		case "linovelib":
			timeout = maxDuration(timeout, 3*time.Minute)
		case "aaatxt":
			timeout = maxDuration(timeout, 90*time.Second)
		}
	}
	return timeout
}

func detailTimeoutForSite(siteKey string) time.Duration {
	return searchTimeoutForSites([]string{siteKey})
}

func chapterContentTimeoutForSite(siteKey string) time.Duration {
	timeout := detailTimeoutForSite(siteKey)
	switch strings.ToLower(strings.TrimSpace(siteKey)) {
	case "alicesw":
		return 15 * time.Second
	default:
		if timeout < 30*time.Second {
			timeout = 30 * time.Second
		}
		return timeout
	}
}

func hideWebSource(siteKey string) bool {
	switch strings.ToLower(strings.TrimSpace(siteKey)) {
	case "tongrenshe":
		return true
	default:
		return false
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

// sourceSpeedRequest is the body for /api/sources/speedtest. All fields are
// optional: when sites is empty every available downloadable source is tested,
// the keyword falls back to a generic Chinese probe, and the timeout uses a
// safe default tuned for slower mirrors.
type sourceSpeedRequest struct {
	Keyword          string   `json:"keyword"`
	Sites            []string `json:"sites"`
	PerSiteTimeoutMs int      `json:"per_site_timeout_ms"`
}

// sourceSpeedResult is the per-site outcome.
type sourceSpeedResult struct {
	Site      string `json:"site"`
	OK        bool   `json:"ok"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Count     int    `json:"count"`
	Error     string `json:"error,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
}

// sourceSpeedResponse aggregates results across the tested sites.
type sourceSpeedResponse struct {
	Keyword string              `json:"keyword"`
	Results []sourceSpeedResult `json:"results"`
}

const (
	sourceSpeedDefaultKeyword     = "测试"
	sourceSpeedDefaultTimeout     = 8 * time.Second
	sourceSpeedMaxTimeout         = 30 * time.Second
	sourceSpeedSearchResultsLimit = 1
)

// runSourceSpeedTest fans out a one-shot Search call per site and reports the
// elapsed time, returning a stable shape regardless of partial failures.
func (s *Service) runSourceSpeedTest(parent context.Context, req sourceSpeedRequest) sourceSpeedResponse {
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		keyword = sourceSpeedDefaultKeyword
	}

	allowed := descriptorKeySet(s.AllSources)
	sites := normalizeSites(req.Sites)
	if len(sites) > 0 {
		sites = filterAllowedSites(sites, allowed)
	}
	if len(sites) == 0 {
		sites = descriptorKeys(s.AllSources)
	}
	if len(sites) == 0 {
		return sourceSpeedResponse{Keyword: keyword, Results: []sourceSpeedResult{}}
	}

	timeout := time.Duration(req.PerSiteTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = sourceSpeedDefaultTimeout
	}
	if timeout > sourceSpeedMaxTimeout {
		timeout = sourceSpeedMaxTimeout
	}

	results := make([]sourceSpeedResult, len(sites))
	var wg sync.WaitGroup
	for idx, siteKey := range sites {
		idx, siteKey := idx, siteKey
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[idx] = s.measureSiteSpeed(parent, siteKey, keyword, timeout)
		}()
	}
	wg.Wait()
	return sourceSpeedResponse{Keyword: keyword, Results: results}
}

func (s *Service) measureSiteSpeed(parent context.Context, siteKey, keyword string, timeout time.Duration) sourceSpeedResult {
	out := sourceSpeedResult{Site: siteKey}
	if s.Runtime == nil || s.Runtime.Registry == nil {
		out.Error = "runtime is not initialised"
		return out
	}
	resolved := s.Config.ResolveSiteConfig(siteKey)
	client, err := s.Runtime.Registry.Build(siteKey, resolved)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	start := time.Now()
	items, err := client.Search(ctx, keyword, sourceSpeedSearchResultsLimit)
	out.ElapsedMs = time.Since(start).Milliseconds()
	if err != nil {
		out.Error = err.Error()
		if ctx.Err() != nil {
			out.TimedOut = true
		}
		return out
	}
	out.OK = true
	out.Count = len(items)
	return out
}
