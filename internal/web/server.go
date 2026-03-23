package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os/exec"
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

type Service struct {
	Config         *config.Config
	Runtime        *app.Runtime
	DefaultSources []site.SiteDescriptor
	AllSources     []site.SiteDescriptor
	Tasks          *DownloadTaskStore
}

type searchRequest struct {
	Keyword   string   `json:"keyword"`
	Scope     string   `json:"scope"`
	Sites     []string `json:"sites"`
	Limit     int      `json:"limit"`
	SiteLimit int      `json:"site_limit"`
}

type downloadRequest struct {
	Site    string   `json:"site"`
	BookID  string   `json:"book_id"`
	Formats []string `json:"formats"`
}

func Start(port string, shouldOpenBrowser bool, configPath string) error {
	service, err := newService(configPath)
	if err != nil {
		return err
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

	defaultSources := make([]site.SiteDescriptor, 0)
	for _, descriptor := range runtime.SiteDescriptors() {
		if descriptor.DefaultAvailable && descriptor.Capabilities.Search {
			defaultSources = append(defaultSources, descriptor)
		}
	}

	allSources := make([]site.SiteDescriptor, 0, len(runtime.SiteDescriptors()))
	for _, descriptor := range runtime.SiteDescriptors() {
		if descriptor.Capabilities.Search {
			allSources = append(allSources, descriptor)
		}
	}

	return &Service{
		Config:         cfg,
		Runtime:        runtime,
		DefaultSources: defaultSources,
		AllSources:     allSources,
		Tasks:          NewDownloadTaskStore(),
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
		})
	})
	group.GET("/style.css", func(c *gin.Context) {
		c.FileFromFS("templates/style.css", http.FS(templateFS))
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
	group.POST("/api/search", func(c *gin.Context) {
		var req searchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}

		sites := normalizeSites(req.Sites)
		if len(sites) == 0 {
			if strings.EqualFold(strings.TrimSpace(req.Scope), "all") {
				sites = service.Runtime.AllSearchSites()
			} else {
				sites = service.Runtime.DefaultSearchSites()
			}
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()

		response, err := service.Runtime.HybridSearch(ctx, req.Keyword, app.HybridSearchOptions{
			Sites:        sites,
			OverallLimit: req.Limit,
			PerSiteLimit: req.SiteLimit,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

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

	return router
}

func (s *Service) startDownloadTask(taskID string, req downloadRequest) {
	go func() {
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
