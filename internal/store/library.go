package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type BookSummary struct {
	Site         string
	BookID       string
	Title        string
	Author       string
	Stage        string
	ModifiedAt   time.Time
	ChapterCount int
}

type BookState struct {
	Book        *model.Book
	Stage       string
	ChapterByID map[string]model.Chapter
}

type PipelineState struct {
	LatestStage string            `json:"latest_stage"`
	Stages      []string          `json:"stages"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Library struct {
	baseDir string
}

type chapterDB interface {
	AutoMigrate(dst ...any) error
	ClearChapters() error
	UpsertChapters(rows []chapterRow) error
	ListChapters() ([]chapterRow, error)
	Close() error
}

type bookInfo struct {
	Site         string    `json:"site"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	Description  string    `json:"description,omitempty"`
	SourceURL    string    `json:"source_url,omitempty"`
	CoverURL     string    `json:"cover_url,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	DownloadedAt time.Time `json:"downloaded_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ChapterCount int       `json:"chapter_count"`
}

type chapterRow struct {
	ID         uint   `gorm:"primaryKey"`
	ChapterID  string `gorm:"index:idx_chapter_unique,unique"`
	Title      string
	Content    string `gorm:"type:text"`
	URL        string
	Volume     string
	OrderIndex int `gorm:"index:idx_chapter_unique,unique"`
	Downloaded bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (chapterRow) TableName() string {
	return "chapters"
}

func NewLibrary(baseDir string) *Library {
	return &Library{baseDir: baseDir}
}

func (l *Library) BaseDir() string {
	return l.baseDir
}

func (l *Library) SaveBookStage(site, stage string, book *model.Book) error {
	if book == nil {
		return fmt.Errorf("book is nil")
	}
	if site == "" {
		site = book.Site
	}
	if site == "" {
		return fmt.Errorf("site is required")
	}
	if book.ID == "" {
		return fmt.Errorf("book id is required")
	}
	if stage == "" {
		stage = "raw"
	}

	bookDir := l.bookDir(site, book.ID)
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		return err
	}

	meta := bookInfo{
		Site:         site,
		ID:           book.ID,
		Title:        book.Title,
		Author:       book.Author,
		Description:  book.Description,
		SourceURL:    book.SourceURL,
		CoverURL:     book.CoverURL,
		Tags:         append([]string(nil), book.Tags...),
		DownloadedAt: book.DownloadedAt,
		UpdatedAt:    book.UpdatedAt,
		ChapterCount: len(book.Chapters),
	}

	if err := writeJSON(l.infoPath(site, book.ID, stage), meta); err != nil {
		return err
	}
	if err := l.writeChaptersSQLite(site, book.ID, stage, book.Chapters); err != nil {
		return err
	}

	state, err := l.LoadPipeline(site, book.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		state = PipelineState{}
	}
	state.LatestStage = stage
	state.UpdatedAt = time.Now().UTC()
	state.Stages = appendUnique(state.Stages, stage)
	if err := writeJSON(l.pipelinePath(site, book.ID), state); err != nil {
		return err
	}

	return nil
}

func (l *Library) LoadBook(site, bookID, stage string) (*model.Book, string, error) {
	if site == "" || bookID == "" {
		return nil, "", fmt.Errorf("site and book id are required")
	}

	if stage == "" || stage == "latest" {
		latest, err := l.LatestStage(site, bookID)
		if err != nil {
			return nil, "", err
		}
		stage = latest
	}

	var info bookInfo
	if err := readJSON(l.infoPath(site, bookID, stage), &info); err != nil {
		return nil, "", err
	}

	chapters, err := l.readChaptersSQLite(site, bookID, stage)
	if err != nil {
		return nil, "", err
	}

	book := &model.Book{
		Site:         info.Site,
		ID:           info.ID,
		Title:        info.Title,
		Author:       info.Author,
		Description:  info.Description,
		SourceURL:    info.SourceURL,
		CoverURL:     info.CoverURL,
		Tags:         append([]string(nil), info.Tags...),
		DownloadedAt: info.DownloadedAt,
		UpdatedAt:    info.UpdatedAt,
		Chapters:     chapters,
	}
	return book, stage, nil
}

func (l *Library) LoadBookState(site, bookID, stage string) (*BookState, error) {
	book, usedStage, err := l.LoadBook(site, bookID, stage)
	if err != nil {
		return nil, err
	}
	state := &BookState{Book: book, Stage: usedStage, ChapterByID: make(map[string]model.Chapter, len(book.Chapters))}
	for _, chapter := range book.Chapters {
		state.ChapterByID[chapter.ID] = chapter
	}
	return state, nil
}

func (l *Library) LoadPipeline(site, bookID string) (PipelineState, error) {
	var state PipelineState
	err := readJSON(l.pipelinePath(site, bookID), &state)
	return state, err
}

func (l *Library) LatestStage(site, bookID string) (string, error) {
	state, err := l.LoadPipeline(site, bookID)
	if err != nil {
		return "", err
	}
	if state.LatestStage == "" {
		return "", fmt.Errorf("no stage recorded for %s/%s", site, bookID)
	}
	return state.LatestStage, nil
}

func (l *Library) ListSites() ([]string, error) {
	entries, err := os.ReadDir(l.baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sites := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			sites = append(sites, entry.Name())
		}
	}
	sort.Strings(sites)
	return sites, nil
}

func (l *Library) ListBooks(site string) ([]BookSummary, error) {
	entries, err := os.ReadDir(filepath.Join(l.baseDir, site))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	books := make([]BookSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		bookID := entry.Name()
		book, stage, err := l.LoadBook(site, bookID, "latest")
		if err != nil {
			continue
		}
		books = append(books, BookSummary{
			Site:         site,
			BookID:       bookID,
			Title:        book.Title,
			Author:       book.Author,
			Stage:        stage,
			ModifiedAt:   book.UpdatedAt,
			ChapterCount: len(book.Chapters),
		})
	}

	sort.Slice(books, func(i, j int) bool {
		return books[i].BookID < books[j].BookID
	})
	return books, nil
}

func (l *Library) RemoveAll(site string) error {
	target := l.baseDir
	if site != "" {
		target = filepath.Join(l.baseDir, site)
	}
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.RemoveAll(target)
}

func (l *Library) RemoveBook(site, bookID, stage string, removeChapters, removeMetadata, removeMedia, removeAll bool) error {
	bookDir := l.bookDir(site, bookID)
	if removeAll {
		if _, err := os.Stat(bookDir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		return os.RemoveAll(bookDir)
	}

	if stage == "" {
		stage = "raw"
	}
	if !removeChapters && !removeMetadata && !removeMedia {
		removeChapters = true
	}

	if removeMetadata {
		_ = os.Remove(l.infoPath(site, bookID, stage))
	}
	if removeChapters {
		_ = os.Remove(l.chaptersPath(site, bookID, stage))
	}
	if removeMedia {
		_ = os.RemoveAll(filepath.Join(bookDir, "media"))
	}

	state, err := l.LoadPipeline(site, bookID)
	if err == nil {
		state.Stages = removeValue(state.Stages, stage)
		if state.LatestStage == stage {
			if len(state.Stages) > 0 {
				state.LatestStage = state.Stages[len(state.Stages)-1]
			} else {
				state.LatestStage = ""
			}
		}
		if len(state.Stages) == 0 && !hasBookArtifacts(bookDir) {
			_ = os.Remove(l.pipelinePath(site, bookID))
		} else {
			state.UpdatedAt = time.Now().UTC()
			_ = writeJSON(l.pipelinePath(site, bookID), state)
		}
	}

	if !hasBookArtifacts(bookDir) {
		_ = os.RemoveAll(bookDir)
	}
	return nil
}

func (l *Library) bookDir(site, bookID string) string {
	return filepath.Join(l.baseDir, sanitize(site), sanitize(bookID))
}

func (l *Library) infoPath(site, bookID, stage string) string {
	return filepath.Join(l.bookDir(site, bookID), fmt.Sprintf("book_info.%s.json", sanitize(stage)))
}

func (l *Library) chaptersPath(site, bookID, stage string) string {
	return filepath.Join(l.bookDir(site, bookID), fmt.Sprintf("chapters.%s.sqlite", sanitize(stage)))
}

func (l *Library) pipelinePath(site, bookID string) string {
	return filepath.Join(l.bookDir(site, bookID), "pipeline.json")
}

func (l *Library) writeChaptersSQLite(site, bookID, stage string, chapters []model.Chapter) error {
	db, err := openChapterDB(l.chaptersPath(site, bookID, stage))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.AutoMigrate(&chapterRow{}); err != nil {
		return err
	}
	if err := db.ClearChapters(); err != nil {
		return err
	}

	rows := make([]chapterRow, 0, len(chapters))
	for idx, chapter := range chapters {
		rows = append(rows, chapterRow{
			ChapterID:  chapter.ID,
			Title:      chapter.Title,
			Content:    chapter.Content,
			URL:        chapter.URL,
			Volume:     chapter.Volume,
			OrderIndex: chooseOrder(chapter.Order, idx+1),
			Downloaded: chapter.Downloaded || chapter.Content != "",
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return db.UpsertChapters(rows)
}

func (l *Library) readChaptersSQLite(site, bookID, stage string) ([]model.Chapter, error) {
	path := l.chaptersPath(site, bookID, stage)
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := openChapterDB(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.ListChapters()
	if err != nil {
		return nil, err
	}
	chapters := make([]model.Chapter, 0, len(rows))
	for _, row := range rows {
		chapters = append(chapters, model.Chapter{
			ID:         row.ChapterID,
			Title:      row.Title,
			Content:    row.Content,
			URL:        row.URL,
			Volume:     row.Volume,
			Order:      row.OrderIndex,
			Downloaded: row.Downloaded,
		})
	}
	return chapters, nil
}

type gormChapterDB struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

func openChapterDB(path string) (chapterDB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	return &gormChapterDB{db: db, sqlDB: sqlDB}, nil
}

func (g *gormChapterDB) AutoMigrate(dst ...any) error {
	return g.db.AutoMigrate(dst...)
}

func (g *gormChapterDB) ClearChapters() error {
	return g.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&chapterRow{}).Error
}

func (g *gormChapterDB) UpsertChapters(rows []chapterRow) error {
	return g.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error
}

func (g *gormChapterDB) ListChapters() ([]chapterRow, error) {
	var rows []chapterRow
	err := g.db.Order("order_index asc").Find(&rows).Error
	return rows, err
}

func (g *gormChapterDB) Close() error {
	if g.sqlDB == nil {
		return nil
	}
	return g.sqlDB.Close()
}

func chooseOrder(existing, fallback int) int {
	if existing > 0 {
		return existing
	}
	return fallback
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func hasBookArtifacts(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func sanitize(value string) string {
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	value = strings.TrimSpace(replacer.Replace(value))
	if value == "" {
		return "unknown"
	}
	return value
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func removeValue(items []string, target string) []string {
	filtered := items[:0]
	for _, item := range items {
		if item != target {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
