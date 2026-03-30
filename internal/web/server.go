package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goRuntime "runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/guohuiyuan/go-novel-dl/internal/app"
	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/progress"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
	"github.com/guohuiyuan/go-novel-dl/internal/ui"
)

//go:embed templates/*
var templateFS embed.FS

const RoutePrefix = "/novel"

const defaultWebPageSize = 50

type Service struct {
	Config         *config.Config
	Runtime        *app.Runtime
	DefaultSources []site.SiteDescriptor
	AllSources     []site.SiteDescriptor
	Tasks          *DownloadTaskStore
	PageSize       int
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

	return &Service{
		Config:         cfg,
		Runtime:        runtime,
		DefaultSources: defaultSources,
		AllSources:     allSources,
		Tasks:          NewDownloadTaskStore(),
		PageSize:       pageSize,
	}, nil
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

	return router
}

func (s *Service) startDownloadTask(taskID string, req downloadRequest) {
	go func() {
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
	return book, nil
}

type taskReporter struct {
	store  *DownloadTaskStore
	taskID string
}

func (r *taskReporter) OnBookStart(siteKey, bookID, title string, total int) {
	r.store.MarkRunning(r.taskID, siteKey, bookID, title, total)
}

func (r *taskReporter) OnBookProgress(done, total int, chapterTitle string) {
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
	case "biquge345":
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
