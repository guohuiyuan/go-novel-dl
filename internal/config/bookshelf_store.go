package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// bookshelfItem is the gorm-managed row for both folders and books on the bookshelf.
// A folder has Kind="folder"; a book has Kind="book". Folders may contain other
// folders or books via ParentID. ParentID == nil means the item lives at the root.
type bookshelfItem struct {
	ID                   uint   `gorm:"primaryKey"`
	Kind                 string `gorm:"size:16;not null;index"`
	ParentID             *uint  `gorm:"index"`
	Name                 string `gorm:"size:256"`
	Sort                 int    `gorm:"default:0;index"`
	Site                 string `gorm:"size:64;index:idx_bookshelf_site_book"`
	BookID               string `gorm:"size:128;index:idx_bookshelf_site_book"`
	Title                string `gorm:"size:256"`
	Author               string `gorm:"size:128"`
	CoverURL             string `gorm:"type:text"`
	Description          string `gorm:"type:text"`
	LatestChapter        string `gorm:"size:256"`
	SourceURL            string `gorm:"type:text"`
	TotalChapters        int    `gorm:"default:0"`
	CachedChapters       int    `gorm:"default:0"`
	LastReadChapterID    string `gorm:"size:256"`
	LastReadChapterIndex int    `gorm:"default:0"`
	LastReadChapterTitle string `gorm:"size:512"`
	LastReadAt           *time.Time
	AddedAt              time.Time `gorm:"autoCreateTime"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime"`
}

func (bookshelfItem) TableName() string {
	return "bookshelf_items"
}

// BookshelfItemKindFolder/Book are the supported Kind values.
const (
	BookshelfItemKindFolder = "folder"
	BookshelfItemKindBook   = "book"
)

// BookshelfItem is the JSON-serialisable view of a bookshelf entry.
type BookshelfItem struct {
	ID                   uint       `json:"id"`
	Kind                 string     `json:"kind"`
	ParentID             *uint      `json:"parent_id,omitempty"`
	Name                 string     `json:"name,omitempty"`
	Sort                 int        `json:"sort"`
	Site                 string     `json:"site,omitempty"`
	BookID               string     `json:"book_id,omitempty"`
	Title                string     `json:"title,omitempty"`
	Author               string     `json:"author,omitempty"`
	CoverURL             string     `json:"cover_url,omitempty"`
	Description          string     `json:"description,omitempty"`
	LatestChapter        string     `json:"latest_chapter,omitempty"`
	SourceURL            string     `json:"source_url,omitempty"`
	TotalChapters        int        `json:"total_chapters,omitempty"`
	CachedChapters       int        `json:"cached_chapters,omitempty"`
	LastReadChapterID    string     `json:"last_read_chapter_id,omitempty"`
	LastReadChapterIndex int        `json:"last_read_chapter_index,omitempty"`
	LastReadChapterTitle string     `json:"last_read_chapter_title,omitempty"`
	LastReadAt           *time.Time `json:"last_read_at,omitempty"`
	AddedAt              time.Time  `json:"added_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ChildCount           int        `json:"child_count,omitempty"`
}

// BookshelfBookInput is the payload for adding a book to the bookshelf. Title is
// required. Site/BookID together identify the book in the source registry; if
// either is empty the row is treated as a manual entry.
type BookshelfBookInput struct {
	ParentID      *uint
	Site          string
	BookID        string
	Title         string
	Author        string
	CoverURL      string
	Description   string
	LatestChapter string
	SourceURL     string
}

// BookshelfMutation describes a partial update against an existing row. Nil
// fields are left untouched.
type BookshelfMutation struct {
	Name                 *string
	ParentID             *uint
	ClearParent          bool
	Title                *string
	Author               *string
	CoverURL             *string
	Description          *string
	LatestChapter        *string
	SourceURL            *string
	TotalChapters        *int
	CachedChapters       *int
	LastReadChapterID    *string
	LastReadChapterIndex *int
	LastReadChapterTitle *string
	LastReadAt           *time.Time
}

func ensureBookshelfTable(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("bookshelf db is unavailable")
	}
	return db.AutoMigrate(&bookshelfItem{})
}

func bookshelfDB() (*gorm.DB, error) {
	if err := ensureSiteCatalogDB(); err != nil {
		return nil, err
	}
	if siteCatalogDB == nil {
		return nil, fmt.Errorf("bookshelf db unavailable")
	}
	if err := ensureBookshelfTable(siteCatalogDB); err != nil {
		return nil, err
	}
	return siteCatalogDB, nil
}

// ListBookshelfItems returns every item directly under parentID, sorted by
// folders-first then by Sort/AddedAt. Pass nil to list root entries.
func ListBookshelfItems(parentID *uint) ([]BookshelfItem, error) {
	db, err := bookshelfDB()
	if err != nil {
		return nil, err
	}
	var entries []bookshelfItem
	query := db.Order("kind asc, sort asc, added_at asc")
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	if err := query.Find(&entries).Error; err != nil {
		return nil, err
	}
	items := make([]BookshelfItem, 0, len(entries))
	folderIDs := make([]uint, 0, len(entries))
	for _, entry := range entries {
		items = append(items, toBookshelfItem(entry))
		if entry.Kind == BookshelfItemKindFolder {
			folderIDs = append(folderIDs, entry.ID)
		}
	}
	if len(folderIDs) > 0 {
		counts, err := bookshelfChildCounts(db, folderIDs)
		if err != nil {
			return nil, err
		}
		for idx := range items {
			if items[idx].Kind == BookshelfItemKindFolder {
				items[idx].ChildCount = counts[items[idx].ID]
			}
		}
	}
	return items, nil
}

func bookshelfChildCounts(db *gorm.DB, parentIDs []uint) (map[uint]int, error) {
	counts := make(map[uint]int, len(parentIDs))
	if len(parentIDs) == 0 {
		return counts, nil
	}
	type row struct {
		ParentID uint
		Total    int
	}
	var rows []row
	if err := db.Model(&bookshelfItem{}).
		Select("parent_id AS parent_id, COUNT(*) AS total").
		Where("parent_id IN ?", parentIDs).
		Group("parent_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.ParentID] = r.Total
	}
	return counts, nil
}

// GetBookshelfItem returns a single item by ID.
func GetBookshelfItem(id uint) (BookshelfItem, error) {
	if id == 0 {
		return BookshelfItem{}, fmt.Errorf("bookshelf id is required")
	}
	db, err := bookshelfDB()
	if err != nil {
		return BookshelfItem{}, err
	}
	var entry bookshelfItem
	if err := db.Where("id = ?", id).Take(&entry).Error; err != nil {
		return BookshelfItem{}, err
	}
	return toBookshelfItem(entry), nil
}

// FindBookshelfBook locates a book entry by site/book_id. Returns ok=false when
// the book is not on the shelf.
func FindBookshelfBook(siteKey, bookID string) (BookshelfItem, bool, error) {
	siteKey = strings.TrimSpace(siteKey)
	bookID = strings.TrimSpace(bookID)
	if siteKey == "" || bookID == "" {
		return BookshelfItem{}, false, nil
	}
	db, err := bookshelfDB()
	if err != nil {
		return BookshelfItem{}, false, err
	}
	var entry bookshelfItem
	err = db.Where("kind = ? AND site = ? AND book_id = ?", BookshelfItemKindBook, siteKey, bookID).
		Take(&entry).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BookshelfItem{}, false, nil
		}
		return BookshelfItem{}, false, err
	}
	return toBookshelfItem(entry), true, nil
}

// BookshelfBreadcrumb walks parent links upwards and returns the path from the
// root to the supplied item (inclusive). The returned slice is empty when id is
// zero.
func BookshelfBreadcrumb(id uint) ([]BookshelfItem, error) {
	if id == 0 {
		return nil, nil
	}
	db, err := bookshelfDB()
	if err != nil {
		return nil, err
	}
	visited := make(map[uint]struct{})
	var path []BookshelfItem
	cursor := &id
	for cursor != nil {
		if _, seen := visited[*cursor]; seen {
			return nil, fmt.Errorf("bookshelf parent cycle detected at id %d", *cursor)
		}
		visited[*cursor] = struct{}{}
		var entry bookshelfItem
		if err := db.Where("id = ?", *cursor).Take(&entry).Error; err != nil {
			return nil, err
		}
		path = append([]BookshelfItem{toBookshelfItem(entry)}, path...)
		cursor = entry.ParentID
	}
	return path, nil
}

// CreateBookshelfFolder creates a new folder named name under parentID. Empty
// name or duplicates are rejected.
func CreateBookshelfFolder(parentID *uint, name string) (BookshelfItem, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return BookshelfItem{}, fmt.Errorf("folder name is required")
	}
	db, err := bookshelfDB()
	if err != nil {
		return BookshelfItem{}, err
	}
	if parentID != nil {
		return BookshelfItem{}, fmt.Errorf("folder can only be created at bookshelf root")
	}
	entry := bookshelfItem{
		Kind:     BookshelfItemKindFolder,
		ParentID: parentID,
		Name:     name,
	}
	if err := db.Create(&entry).Error; err != nil {
		return BookshelfItem{}, err
	}
	return toBookshelfItem(entry), nil
}

// AddBookshelfBook inserts a book row. If the same site/book_id already exists
// the existing row is moved to the requested parentID (when provided) and any
// missing metadata is filled in. The returned item reflects the post-state.
func AddBookshelfBook(input BookshelfBookInput) (BookshelfItem, error) {
	site := strings.TrimSpace(input.Site)
	bookID := strings.TrimSpace(input.BookID)
	if site == "" || bookID == "" {
		return BookshelfItem{}, fmt.Errorf("site and book_id are required")
	}
	db, err := bookshelfDB()
	if err != nil {
		return BookshelfItem{}, err
	}
	if input.ParentID != nil {
		if err := requireFolderParent(db, *input.ParentID); err != nil {
			return BookshelfItem{}, err
		}
	}

	var existing bookshelfItem
	err = db.Where("kind = ? AND site = ? AND book_id = ?", BookshelfItemKindBook, site, bookID).
		Take(&existing).Error
	if err == nil {
		if input.ParentID != nil {
			existing.ParentID = input.ParentID
		}
		if value := strings.TrimSpace(input.Title); value != "" && existing.Title == "" {
			existing.Title = value
		}
		if value := strings.TrimSpace(input.Author); value != "" && existing.Author == "" {
			existing.Author = value
		}
		if value := strings.TrimSpace(input.CoverURL); value != "" && existing.CoverURL == "" {
			existing.CoverURL = value
		}
		if value := strings.TrimSpace(input.Description); value != "" && existing.Description == "" {
			existing.Description = value
		}
		if value := strings.TrimSpace(input.LatestChapter); value != "" {
			existing.LatestChapter = value
		}
		if value := strings.TrimSpace(input.SourceURL); value != "" && existing.SourceURL == "" {
			existing.SourceURL = value
		}
		if err := db.Save(&existing).Error; err != nil {
			return BookshelfItem{}, err
		}
		return toBookshelfItem(existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return BookshelfItem{}, err
	}

	entry := bookshelfItem{
		Kind:          BookshelfItemKindBook,
		ParentID:      input.ParentID,
		Site:          site,
		BookID:        bookID,
		Title:         strings.TrimSpace(input.Title),
		Author:        strings.TrimSpace(input.Author),
		CoverURL:      strings.TrimSpace(input.CoverURL),
		Description:   strings.TrimSpace(input.Description),
		LatestChapter: strings.TrimSpace(input.LatestChapter),
		SourceURL:     strings.TrimSpace(input.SourceURL),
	}
	if entry.Title == "" {
		entry.Title = bookID
	}
	if err := db.Create(&entry).Error; err != nil {
		return BookshelfItem{}, err
	}
	return toBookshelfItem(entry), nil
}

// UpdateBookshelfItem applies a partial mutation. Use ClearParent=true to move
// an item back to the root.
func UpdateBookshelfItem(id uint, patch BookshelfMutation) (BookshelfItem, error) {
	if id == 0 {
		return BookshelfItem{}, fmt.Errorf("bookshelf id is required")
	}
	db, err := bookshelfDB()
	if err != nil {
		return BookshelfItem{}, err
	}
	var entry bookshelfItem
	if err := db.Where("id = ?", id).Take(&entry).Error; err != nil {
		return BookshelfItem{}, err
	}

	if patch.Name != nil {
		entry.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.ClearParent {
		entry.ParentID = nil
	} else if patch.ParentID != nil {
		if *patch.ParentID == entry.ID {
			return BookshelfItem{}, fmt.Errorf("cannot move an item into itself")
		}
		if entry.Kind == BookshelfItemKindFolder {
			return BookshelfItem{}, fmt.Errorf("folder can only live at bookshelf root")
		}
		if err := requireFolderParent(db, *patch.ParentID); err != nil {
			return BookshelfItem{}, err
		}
		parent := *patch.ParentID
		entry.ParentID = &parent
	}
	if patch.Title != nil {
		entry.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Author != nil {
		entry.Author = strings.TrimSpace(*patch.Author)
	}
	if patch.CoverURL != nil {
		entry.CoverURL = strings.TrimSpace(*patch.CoverURL)
	}
	if patch.Description != nil {
		entry.Description = strings.TrimSpace(*patch.Description)
	}
	if patch.LatestChapter != nil {
		entry.LatestChapter = strings.TrimSpace(*patch.LatestChapter)
	}
	if patch.SourceURL != nil {
		entry.SourceURL = strings.TrimSpace(*patch.SourceURL)
	}
	if patch.TotalChapters != nil {
		value := *patch.TotalChapters
		if value < 0 {
			value = 0
		}
		entry.TotalChapters = value
	}
	if patch.CachedChapters != nil {
		value := *patch.CachedChapters
		if value < 0 {
			value = 0
		}
		entry.CachedChapters = value
	}
	if patch.LastReadChapterID != nil {
		entry.LastReadChapterID = strings.TrimSpace(*patch.LastReadChapterID)
	}
	if patch.LastReadChapterIndex != nil {
		value := *patch.LastReadChapterIndex
		if value < 0 {
			value = 0
		}
		entry.LastReadChapterIndex = value
	}
	if patch.LastReadChapterTitle != nil {
		entry.LastReadChapterTitle = strings.TrimSpace(*patch.LastReadChapterTitle)
	}
	if patch.LastReadAt != nil {
		stamp := *patch.LastReadAt
		entry.LastReadAt = &stamp
	}

	if err := db.Save(&entry).Error; err != nil {
		return BookshelfItem{}, err
	}
	return toBookshelfItem(entry), nil
}

// DeleteBookshelfItem removes an entry. Folders are deleted recursively along
// with all descendants.
func DeleteBookshelfItem(id uint) error {
	if id == 0 {
		return fmt.Errorf("bookshelf id is required")
	}
	db, err := bookshelfDB()
	if err != nil {
		return err
	}
	var entry bookshelfItem
	if err := db.Where("id = ?", id).Take(&entry).Error; err != nil {
		return err
	}
	if entry.Kind == BookshelfItemKindFolder {
		descendants, err := collectDescendants(db, entry.ID)
		if err != nil {
			return err
		}
		ids := make([]uint, 0, len(descendants)+1)
		for _, d := range descendants {
			ids = append(ids, d)
		}
		ids = append(ids, entry.ID)
		return db.Where("id IN ?", ids).Delete(&bookshelfItem{}).Error
	}
	return db.Where("id = ?", entry.ID).Delete(&bookshelfItem{}).Error
}

// BookshelfProgressInput captures everything needed to record a reading
// position. Title is optional (used purely for display in the history feed).
type BookshelfProgressInput struct {
	Site         string
	BookID       string
	ChapterID    string
	ChapterIndex int
	ChapterTitle string
}

// UpdateBookshelfProgress records the latest reading position for a book
// identified by site/book_id. It is a no-op when the book is not on the shelf
// (matches found=false). The returned bool indicates whether an existing row
// was updated.
func UpdateBookshelfProgress(input BookshelfProgressInput) (BookshelfItem, bool, error) {
	site := strings.TrimSpace(input.Site)
	bookID := strings.TrimSpace(input.BookID)
	if site == "" || bookID == "" {
		return BookshelfItem{}, false, fmt.Errorf("site and book_id are required")
	}
	db, err := bookshelfDB()
	if err != nil {
		return BookshelfItem{}, false, err
	}
	var entry bookshelfItem
	err = db.Where("kind = ? AND site = ? AND book_id = ?", BookshelfItemKindBook, site, bookID).Take(&entry).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BookshelfItem{}, false, nil
		}
		return BookshelfItem{}, false, err
	}
	index := input.ChapterIndex
	if index < 0 {
		index = 0
	}
	now := time.Now().UTC()
	entry.LastReadChapterID = strings.TrimSpace(input.ChapterID)
	entry.LastReadChapterIndex = index
	entry.LastReadChapterTitle = strings.TrimSpace(input.ChapterTitle)
	entry.LastReadAt = &now
	if err := db.Save(&entry).Error; err != nil {
		return BookshelfItem{}, false, err
	}
	return toBookshelfItem(entry), true, nil
}

// ListBookshelfHistory returns book entries that have a recorded last read
// timestamp, ordered most-recent-first. The limit caps the slice length; pass
// 0 or negative to apply a sensible default of 50.
func ListBookshelfHistory(limit int) ([]BookshelfItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	db, err := bookshelfDB()
	if err != nil {
		return nil, err
	}
	var entries []bookshelfItem
	if err := db.
		Where("kind = ? AND last_read_at IS NOT NULL", BookshelfItemKindBook).
		Order("last_read_at DESC").
		Limit(limit).
		Find(&entries).Error; err != nil {
		return nil, err
	}
	items := make([]BookshelfItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, toBookshelfItem(entry))
	}
	return items, nil
}

// UpdateBookshelfCacheStats refreshes the cache progress columns for a book by
// site/book_id. It is a no-op when the row does not exist.
func UpdateBookshelfCacheStats(siteKey, bookID string, totalChapters, cachedChapters int) error {
	site := strings.TrimSpace(siteKey)
	id := strings.TrimSpace(bookID)
	if site == "" || id == "" {
		return nil
	}
	db, err := bookshelfDB()
	if err != nil {
		return err
	}
	if totalChapters < 0 {
		totalChapters = 0
	}
	if cachedChapters < 0 {
		cachedChapters = 0
	}
	return db.Model(&bookshelfItem{}).
		Where("kind = ? AND site = ? AND book_id = ?", BookshelfItemKindBook, site, id).
		Updates(map[string]any{
			"total_chapters":  totalChapters,
			"cached_chapters": cachedChapters,
		}).Error
}

func requireFolderParent(db *gorm.DB, parentID uint) error {
	if parentID == 0 {
		return fmt.Errorf("invalid parent id")
	}
	var parent bookshelfItem
	if err := db.Select("id", "kind").Where("id = ?", parentID).Take(&parent).Error; err != nil {
		return fmt.Errorf("parent folder not found: %w", err)
	}
	if parent.Kind != BookshelfItemKindFolder {
		return fmt.Errorf("parent must be a folder")
	}
	return nil
}

func ensureNotInSubtree(db *gorm.DB, rootID, candidateID uint) error {
	descendants, err := collectDescendants(db, rootID)
	if err != nil {
		return err
	}
	for _, id := range descendants {
		if id == candidateID {
			return fmt.Errorf("cannot move a folder into its own descendant")
		}
	}
	return nil
}

func collectDescendants(db *gorm.DB, rootID uint) ([]uint, error) {
	queue := []uint{rootID}
	descendants := make([]uint, 0, 8)
	for len(queue) > 0 {
		current := queue
		queue = nil
		var rows []bookshelfItem
		if err := db.Select("id").Where("parent_id IN ?", current).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			descendants = append(descendants, row.ID)
			queue = append(queue, row.ID)
		}
	}
	return descendants, nil
}

func toBookshelfItem(entry bookshelfItem) BookshelfItem {
	return BookshelfItem{
		ID:                   entry.ID,
		Kind:                 entry.Kind,
		ParentID:             entry.ParentID,
		Name:                 entry.Name,
		Sort:                 entry.Sort,
		Site:                 entry.Site,
		BookID:               entry.BookID,
		Title:                entry.Title,
		Author:               entry.Author,
		CoverURL:             entry.CoverURL,
		Description:          entry.Description,
		LatestChapter:        entry.LatestChapter,
		SourceURL:            entry.SourceURL,
		TotalChapters:        entry.TotalChapters,
		CachedChapters:       entry.CachedChapters,
		LastReadChapterID:    entry.LastReadChapterID,
		LastReadChapterIndex: entry.LastReadChapterIndex,
		LastReadChapterTitle: entry.LastReadChapterTitle,
		LastReadAt:           entry.LastReadAt,
		AddedAt:              entry.AddedAt,
		UpdatedAt:            entry.UpdatedAt,
	}
}
