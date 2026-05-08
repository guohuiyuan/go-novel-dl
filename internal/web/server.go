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
}

type bookDetailResponse struct {
	Book model.Book `json:"book"`
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
	defaultSources := allSources

	pageSize := cfg.General.WebPageSize
	if pageSize <= 0 {
		pageSize = defaultWebPageSize
	}

	warnings := collectSiteWarnings(runtime)
	stats := collectSiteStats(runtime)
	siteConfigs, _ := config.ListSiteCatalog()
	paramSupports := config.SiteParameterSupports()
	generalConfig, _ := config.LoadGeneralConfig()

	return &Service{
		Config:         cfg,
		ConfigPath:     configPath,
		GeneralConfig:  generalConfig,
		Runtime:        runtime,
		DefaultSources: defaultSources,
		AllSources:     allSources,
		Tasks:          NewDownloadTaskStore(),
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
	return nil
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

		if _, ok := descriptorKeySet(service.AllSources)[siteKey]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "selected site must support both search and download"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), detailTimeoutForSite(siteKey))
		defer cancel()

		book, err := service.bookDetail(ctx, siteKey, bookID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, bookDetailResponse{Book: *book})
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

		task := service.Tasks.Create(req.Site, req.BookID)
		service.startDownloadTask(task.ID, req)

		c.JSON(http.StatusAccepted, gin.H{
			"task": task,
		})
	})
	group.GET("/api/download-tasks/:id", func(c *gin.Context) {
		task, ok := service.Tasks.Snapshot(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"task": task})
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

		timeout := detailTimeoutForSite(siteKey)
		if timeout < 30*time.Second {
			timeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		ch := model.Chapter{ID: chapterID, Title: chapterTitle, URL: chapterURL}

		result, err := service.chapterContent(ctx, siteKey, bookID, ch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"chapter": result})
	})

	return router
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
	if _, ok := descriptorKeySet(s.AllSources)[resolved.SiteKey]; !ok {
		return paginatedSearchResponse{}, fmt.Errorf("该链接所属站点当前不支持 Web 搜索下载")
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

		runtime := s.newTaskRuntime(taskID)
		results, err := runtime.Download(context.Background(), req.Site, []model.BookRef{{
			BookID: req.BookID,
		}}, req.Formats, false)
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
		for _, result := range results {
			exported = append(exported, result.Exported...)
			if strings.TrimSpace(title) == "" && result.Book != nil {
				title = result.Book.Title
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
		case "biquge5", "piaotia":
			timeout = maxDuration(timeout, 45*time.Second)
		case "linovelib":
			timeout = maxDuration(timeout, 3*time.Minute)
		}
	}
	return timeout
}

func detailTimeoutForSite(siteKey string) time.Duration {
	return searchTimeoutForSites([]string{siteKey})
}

func hideWebSource(siteKey string) bool {
	switch strings.ToLower(strings.TrimSpace(siteKey)) {
	case "biquge345", "tongrenshe":
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
