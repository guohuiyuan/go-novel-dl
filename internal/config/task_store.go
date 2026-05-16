package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// downloadTaskRow stores a persisted download task. Lists/objects are stored
// as JSON text columns to avoid schema churn.
type downloadTaskRow struct {
	ID                string `gorm:"primaryKey;size:128"`
	Site              string `gorm:"size:64;index"`
	BookID            string `gorm:"size:128;index"`
	Title             string `gorm:"size:256"`
	Status            string `gorm:"size:32;index"`
	Phase             string `gorm:"size:32"`
	Target            string `gorm:"size:32;index"`
	Formats           string `gorm:"type:text"`
	TotalChapters     int
	CompletedChapters int
	CurrentChapter    string
	ETA               string `gorm:"size:64"`
	Speed             float64
	Exported          string `gorm:"type:text"`
	Messages          string `gorm:"type:text"`
	Error             string `gorm:"type:text"`
	StartTime         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	FinishedAt        *time.Time
}

func (downloadTaskRow) TableName() string {
	return "download_tasks"
}

// DownloadTaskTarget controls what the worker does with a download task. shelf
// = persist into local library cache only (no exporter); browser = also export
// to the configured output directory and surface download links.
const (
	DownloadTaskTargetBrowser = "browser"
	DownloadTaskTargetShelf   = "shelf"
)

// DownloadTaskMessageRecord mirrors the in-memory DownloadTaskMessage to keep
// JSON serialisation stable across packages.
type DownloadTaskMessageRecord struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

// DownloadTaskRecord is the persisted snapshot of a download task.
type DownloadTaskRecord struct {
	ID                string                      `json:"id"`
	Site              string                      `json:"site"`
	BookID            string                      `json:"book_id"`
	Title             string                      `json:"title,omitempty"`
	Status            string                      `json:"status"`
	Phase             string                      `json:"phase"`
	Target            string                      `json:"target,omitempty"`
	Formats           []string                    `json:"formats,omitempty"`
	TotalChapters     int                         `json:"total_chapters"`
	CompletedChapters int                         `json:"completed_chapters"`
	CurrentChapter    string                      `json:"current_chapter,omitempty"`
	ETA               string                      `json:"eta,omitempty"`
	Speed             float64                     `json:"speed,omitempty"`
	Exported          []string                    `json:"exported,omitempty"`
	Messages          []DownloadTaskMessageRecord `json:"messages,omitempty"`
	Error             string                      `json:"error,omitempty"`
	StartTime         time.Time                   `json:"start_time,omitempty"`
	CreatedAt         time.Time                   `json:"created_at"`
	UpdatedAt         time.Time                   `json:"updated_at"`
	FinishedAt        *time.Time                  `json:"finished_at,omitempty"`
}

func ensureDownloadTaskTable(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("download task db is unavailable")
	}
	return db.AutoMigrate(&downloadTaskRow{})
}

func taskDB() (*gorm.DB, error) {
	if err := ensureSiteCatalogDB(); err != nil {
		return nil, err
	}
	if siteCatalogDB == nil {
		return nil, fmt.Errorf("download task db unavailable")
	}
	if err := ensureDownloadTaskTable(siteCatalogDB); err != nil {
		return nil, err
	}
	return siteCatalogDB, nil
}

// SaveDownloadTask upserts a task record. Empty task IDs are rejected.
func SaveDownloadTask(record DownloadTaskRecord) error {
	id := strings.TrimSpace(record.ID)
	if id == "" {
		return fmt.Errorf("task id is required")
	}
	db, err := taskDB()
	if err != nil {
		return err
	}
	row, err := taskRowFromRecord(record)
	if err != nil {
		return err
	}
	row.ID = id
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	row.UpdatedAt = time.Now().UTC()
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&row).Error
}

// LoadDownloadTask returns the persisted task with the supplied ID.
func LoadDownloadTask(id string) (DownloadTaskRecord, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return DownloadTaskRecord{}, false, nil
	}
	db, err := taskDB()
	if err != nil {
		return DownloadTaskRecord{}, false, err
	}
	var row downloadTaskRow
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DownloadTaskRecord{}, false, nil
		}
		return DownloadTaskRecord{}, false, err
	}
	rec, err := taskRecordFromRow(row)
	if err != nil {
		return DownloadTaskRecord{}, false, err
	}
	return rec, true, nil
}

// ListDownloadTasks returns all persisted tasks ordered by most recent update.
func ListDownloadTasks() ([]DownloadTaskRecord, error) {
	db, err := taskDB()
	if err != nil {
		return nil, err
	}
	var rows []downloadTaskRow
	if err := db.Order("updated_at desc, created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]DownloadTaskRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := taskRecordFromRow(row)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

// DeleteDownloadTask removes a single task. Missing IDs are ignored.
func DeleteDownloadTask(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	db, err := taskDB()
	if err != nil {
		return err
	}
	return db.Where("id = ?", id).Delete(&downloadTaskRow{}).Error
}

// MarkOrphanedDownloadTasksFailed flags every task currently in queued/running
// state as failed. Use this on startup so the UI does not display tasks that
// can never finish in this process.
func MarkOrphanedDownloadTasksFailed(reason string) (int, error) {
	db, err := taskDB()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	message := DownloadTaskMessageRecord{At: now, Level: "error", Text: strings.TrimSpace(reason)}
	if message.Text == "" {
		message.Text = "进程已重启，任务被中断"
	}
	var rows []downloadTaskRow
	if err := db.Where("status IN ?", []string{"queued", "running"}).Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	for _, row := range rows {
		messages, _ := decodeMessages(row.Messages)
		messages = append(messages, message)
		encoded, err := encodeMessages(messages)
		if err != nil {
			return 0, err
		}
		row.Status = "failed"
		row.Phase = "failed"
		row.Error = message.Text
		finishedAt := now
		row.FinishedAt = &finishedAt
		row.UpdatedAt = now
		row.Messages = encoded
		if err := db.Save(&row).Error; err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

func taskRowFromRecord(record DownloadTaskRecord) (downloadTaskRow, error) {
	formats, err := encodeStrings(record.Formats)
	if err != nil {
		return downloadTaskRow{}, err
	}
	exported, err := encodeStrings(record.Exported)
	if err != nil {
		return downloadTaskRow{}, err
	}
	messages, err := encodeMessages(record.Messages)
	if err != nil {
		return downloadTaskRow{}, err
	}
	row := downloadTaskRow{
		ID:                record.ID,
		Site:              record.Site,
		BookID:            record.BookID,
		Title:             record.Title,
		Status:            record.Status,
		Phase:             record.Phase,
		Target:            record.Target,
		Formats:           formats,
		TotalChapters:     record.TotalChapters,
		CompletedChapters: record.CompletedChapters,
		CurrentChapter:    record.CurrentChapter,
		ETA:               record.ETA,
		Speed:             record.Speed,
		Exported:          exported,
		Messages:          messages,
		Error:             record.Error,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
		FinishedAt:        record.FinishedAt,
	}
	if !record.StartTime.IsZero() {
		stamp := record.StartTime
		row.StartTime = &stamp
	}
	return row, nil
}

func taskRecordFromRow(row downloadTaskRow) (DownloadTaskRecord, error) {
	formats, err := decodeStrings(row.Formats)
	if err != nil {
		return DownloadTaskRecord{}, err
	}
	exported, err := decodeStrings(row.Exported)
	if err != nil {
		return DownloadTaskRecord{}, err
	}
	messages, err := decodeMessages(row.Messages)
	if err != nil {
		return DownloadTaskRecord{}, err
	}
	rec := DownloadTaskRecord{
		ID:                row.ID,
		Site:              row.Site,
		BookID:            row.BookID,
		Title:             row.Title,
		Status:            row.Status,
		Phase:             row.Phase,
		Target:            row.Target,
		Formats:           formats,
		TotalChapters:     row.TotalChapters,
		CompletedChapters: row.CompletedChapters,
		CurrentChapter:    row.CurrentChapter,
		ETA:               row.ETA,
		Speed:             row.Speed,
		Exported:          exported,
		Messages:          messages,
		Error:             row.Error,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		FinishedAt:        row.FinishedAt,
	}
	if row.StartTime != nil {
		rec.StartTime = *row.StartTime
	}
	return rec, nil
}

func encodeStrings(values []string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeStrings(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeMessages(messages []DownloadTaskMessageRecord) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeMessages(raw string) ([]DownloadTaskMessageRecord, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []DownloadTaskMessageRecord
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
