package web

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

const maxTaskMessages = 200

type DownloadTaskMessage struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

// DownloadTaskTarget controls how a task's output is delivered. local writes
// the book into the server-side library; export reads the local library and
// generates downloadable files. browser/shelf are kept as legacy aliases.
const (
	DownloadTaskTargetLocal   = "local"
	DownloadTaskTargetExport  = "export"
	DownloadTaskTargetBrowser = "browser"
	DownloadTaskTargetShelf   = "shelf"
)

type DownloadTask struct {
	ID                string                `json:"id"`
	Site              string                `json:"site"`
	BookID            string                `json:"book_id"`
	Title             string                `json:"title,omitempty"`
	Status            string                `json:"status"`
	Phase             string                `json:"phase"`
	Target            string                `json:"target,omitempty"`
	Formats           []string              `json:"formats,omitempty"`
	TotalChapters     int                   `json:"total_chapters"`
	CompletedChapters int                   `json:"completed_chapters"`
	CurrentChapter    string                `json:"current_chapter,omitempty"`
	ETA               string                `json:"eta,omitempty"`
	Speed             float64               `json:"speed,omitempty"`
	Exported          []string              `json:"exported,omitempty"`
	Error             string                `json:"error,omitempty"`
	Messages          []DownloadTaskMessage `json:"messages,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
	FinishedAt        *time.Time            `json:"finished_at,omitempty"`
	StartTime         time.Time             `json:"start_time,omitempty"`
	rateSamples       []rateSample          `json:"-"`
}

// rateSample is one (timestamp, completed_chapters) observation that the
// sliding-window estimator keeps.
type rateSample struct {
	at   time.Time
	done int
}

const (
	// rateWindowDuration is the rolling window used to estimate the current
	// download rate. Long enough to absorb chapter-to-chapter jitter (rate
	// limits, occasional 502s, slow chapters), short enough to react when the
	// site genuinely speeds up or slows down. 30s mirrors what aria2c, wget
	// and most browser progress bars use.
	rateWindowDuration = 30 * time.Second

	// rateMaxSamples bounds memory for very chatty progress reporters. A
	// task that emits 10/s for 30 minutes won't grow this slice unboundedly.
	rateMaxSamples = 256
)

// DownloadTaskOptions describes optional fields when creating a download task.
type DownloadTaskOptions struct {
	Target  string
	Formats []string
}

// taskPersister abstracts the persistence layer so tests can run without a
// SQLite connection while production code wires the config-backed store via
// HydrateFromConfig.
type taskPersister interface {
	Save(record config.DownloadTaskRecord) error
	Delete(id string) error
}

type noopTaskPersister struct{}

func (noopTaskPersister) Save(_ config.DownloadTaskRecord) error { return nil }
func (noopTaskPersister) Delete(_ string) error                  { return nil }

type configTaskPersister struct{}

func (configTaskPersister) Save(record config.DownloadTaskRecord) error {
	return config.SaveDownloadTask(record)
}

func (configTaskPersister) Delete(id string) error {
	return config.DeleteDownloadTask(id)
}

type DownloadTaskStore struct {
	mu        sync.RWMutex
	seq       uint64
	tasks     map[string]*DownloadTask
	persister taskPersister
}

func NewDownloadTaskStore() *DownloadTaskStore {
	return &DownloadTaskStore{
		tasks:     make(map[string]*DownloadTask),
		persister: noopTaskPersister{},
	}
}

// HydrateFromConfig switches the store to the SQLite-backed persister, loads
// any tasks the previous process left behind, and marks queued/running tasks
// as failed (we cannot resume them after a restart). Returns the number of
// orphaned tasks that were transitioned to failed.
func (s *DownloadTaskStore) HydrateFromConfig() (int, error) {
	orphans, err := config.MarkOrphanedDownloadTasksFailed("进程已重启，任务被中断")
	if err != nil {
		return 0, err
	}
	records, err := config.ListDownloadTasks()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.persister = configTaskPersister{}
	for _, record := range records {
		s.tasks[record.ID] = recordToTask(record)
	}
	s.mu.Unlock()
	return orphans, nil
}

func (s *DownloadTaskStore) Create(siteKey string, bookID string, opts ...DownloadTaskOptions) DownloadTask {
	now := time.Now().UTC()
	id := fmt.Sprintf("task-%d-%d", now.UnixNano(), atomic.AddUint64(&s.seq, 1))

	var opt DownloadTaskOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	target := strings.TrimSpace(opt.Target)
	if target == "" {
		target = DownloadTaskTargetBrowser
	}

	task := &DownloadTask{
		ID:        id,
		Site:      strings.TrimSpace(siteKey),
		BookID:    strings.TrimSpace(bookID),
		Status:    "queued",
		Phase:     "queued",
		Target:    target,
		Formats:   append([]string(nil), opt.Formats...),
		CreatedAt: now,
		UpdatedAt: now,
		Messages: []DownloadTaskMessage{{
			At:    now,
			Level: "info",
			Text:  "任务已排队",
		}},
	}

	s.mu.Lock()
	s.tasks[id] = task
	persister := s.persister
	snapshot := cloneTask(task)
	s.mu.Unlock()

	if persister != nil {
		_ = persister.Save(taskRecordFromSnapshot(snapshot))
	}
	return snapshot
}

func (s *DownloadTaskStore) Snapshot(id string) (DownloadTask, bool) {
	s.mu.RLock()
	task, ok := s.tasks[id]
	s.mu.RUnlock()
	if !ok {
		return DownloadTask{}, false
	}
	return cloneTask(task), true
}

// List returns a stable snapshot of every task currently in the store.
func (s *DownloadTaskStore) List() []DownloadTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DownloadTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		out = append(out, cloneTask(task))
	}
	return out
}

// Delete removes a task from the store and the persistent backend. Returns
// false when the task does not exist.
func (s *DownloadTaskStore) Delete(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	s.mu.Lock()
	_, ok := s.tasks[id]
	if ok {
		delete(s.tasks, id)
	}
	persister := s.persister
	s.mu.Unlock()
	if persister != nil {
		_ = persister.Delete(id)
	}
	return ok
}

func (s *DownloadTaskStore) MarkRunning(id string, siteKey string, bookID string, title string, total int) {
	now := time.Now().UTC()
	s.update(id, func(task *DownloadTask) {
		task.Status = "running"
		task.Phase = "downloading"
		task.Site = siteKey
		task.BookID = bookID
		if strings.TrimSpace(title) != "" {
			task.Title = title
		}
		task.TotalChapters = total
		task.CompletedChapters = 0
		task.CurrentChapter = ""
		if task.StartTime.IsZero() {
			task.StartTime = now
		}
		task.rateSamples = nil
		task.Speed = 0
		task.ETA = ""
		appendTaskMessage(task, "info", fmt.Sprintf("开始下载（%d章）", total))
	})
}

func (s *DownloadTaskStore) MarkLoadingChapters(id string, siteKey string, bookID string) {
	s.update(id, func(task *DownloadTask) {
		now := time.Now().UTC()
		task.Status = "running"
		task.Phase = "loading_chapters"
		task.Site = siteKey
		task.BookID = bookID
		if task.StartTime.IsZero() {
			task.StartTime = now
		}
		appendTaskMessage(task, "info", "正在加载章节列表...")
	})
}

func (s *DownloadTaskStore) MarkProgress(id string, done int, total int, chapterTitle string) {
	s.update(id, func(task *DownloadTask) {
		task.Status = "running"
		task.Phase = "downloading"
		task.CompletedChapters = done
		if total > 0 {
			task.TotalChapters = total
		}
		task.CurrentChapter = strings.TrimSpace(chapterTitle)

		now := time.Now().UTC()
		samples, rate := updateRateWindow(task.rateSamples, now, done, rateWindowDuration, rateMaxSamples)
		task.rateSamples = samples

		if rate > 0 && isFiniteFloat(rate) {
			task.Speed = rate
			remaining := task.TotalChapters - done
			if remaining < 0 {
				remaining = 0
			}
			etaSeconds := float64(remaining) / rate
			if etaSeconds >= 0 && isFiniteFloat(etaSeconds) {
				task.ETA = formatETADuration(time.Duration(etaSeconds * float64(time.Second)))
			} else {
				task.ETA = ""
			}
		} else {
			// Not enough recent data (just started, stalled, or no forward
			// motion within the window). Surface no estimate rather than a
			// stale or made-up one.
			task.Speed = 0
			task.ETA = ""
		}

		message := fmt.Sprintf("已处理章节 %d/%d", done, task.TotalChapters)
		if task.CurrentChapter != "" {
			message += ": " + task.CurrentChapter
		}
		appendTaskMessage(task, "progress", message)
	})
}

// updateRateWindow appends the latest progress observation, evicts samples
// older than windowDuration (always keeping the newest), and returns the
// chapter-per-second rate inferred from the remaining buffer.
//
// The estimate is the slope between the oldest in-window sample and the
// newest one — a true windowed rate, not an EMA. When the window contains
// less than two samples, or no forward motion, the function returns rate=0
// so callers can hide the ETA instead of fabricating one.
func updateRateWindow(samples []rateSample, now time.Time, done int, windowDuration time.Duration, maxSamples int) ([]rateSample, float64) {
	if len(samples) > 0 {
		last := samples[len(samples)-1]
		if done < last.done {
			// Backwards motion is treated as a hard reset (re-run, retry).
			samples = samples[:0]
		} else if done == last.done && !now.After(last.at) {
			// Duplicate event with no forward progress; reuse buffer.
			return samples, computeWindowedRate(samples)
		}
	}
	samples = append(samples, rateSample{at: now, done: done})

	cutoff := now.Add(-windowDuration)
	drop := 0
	// Always keep the newest sample so the buffer never empties; that way the
	// next progress event can immediately compute a fresh rate.
	for drop < len(samples)-1 && samples[drop].at.Before(cutoff) {
		drop++
	}
	if drop > 0 {
		samples = samples[drop:]
	}
	if maxSamples > 0 && len(samples) > maxSamples {
		samples = samples[len(samples)-maxSamples:]
	}
	return samples, computeWindowedRate(samples)
}

func computeWindowedRate(samples []rateSample) float64 {
	if len(samples) < 2 {
		return 0
	}
	first := samples[0]
	last := samples[len(samples)-1]
	dt := last.at.Sub(first.at).Seconds()
	if dt <= 0 {
		return 0
	}
	delta := last.done - first.done
	if delta <= 0 {
		return 0
	}
	rate := float64(delta) / dt
	if !isFiniteFloat(rate) || rate <= 0 {
		return 0
	}
	return rate
}

func formatETADuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d小时%d分", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%d分%d秒", m, s)
	}
	return fmt.Sprintf("%d秒", s)
}

func (s *DownloadTaskStore) MarkExporting(id string, done int, total int) {
	s.update(id, func(task *DownloadTask) {
		task.Status = "running"
		task.Phase = "exporting"
		message := "章节抓取完成，正在导出"
		if task.Target == DownloadTaskTargetLocal || task.Target == DownloadTaskTargetShelf {
			task.Phase = "saving"
			message = "章节下载完成，正在保存到服务器本地"
		}
		if total <= 0 {
			total = task.TotalChapters
		}
		if total <= 0 {
			total = done
		}
		if total <= 0 {
			total = 1
		}
		task.TotalChapters = total + 1
		if done > total {
			done = total
		}
		task.CompletedChapters = done
		task.CurrentChapter = ""
		appendTaskMessage(task, "info", message)
	})
}

func (s *DownloadTaskStore) MarkCompleted(id string, title string, exported []string) {
	s.update(id, func(task *DownloadTask) {
		now := time.Now().UTC()
		task.Status = "completed"
		task.Phase = "completed"
		task.Error = ""
		task.FinishedAt = &now
		if task.TotalChapters <= 0 {
			task.TotalChapters = 1
		}
		task.CompletedChapters = task.TotalChapters
		if strings.TrimSpace(title) != "" {
			task.Title = title
		}
		task.Exported = append([]string(nil), exported...)
		if len(exported) > 0 || task.Target == DownloadTaskTargetExport || task.Target == DownloadTaskTargetBrowser {
			appendTaskMessage(task, "success", fmt.Sprintf("导出完成（%d个文件）", len(exported)))
		} else {
			appendTaskMessage(task, "success", "下载完成，已保存到服务器本地")
		}
		if hasEPUBExport(exported) {
			elapsed := now.Sub(task.StartTime)
			if task.StartTime.IsZero() || elapsed < 0 {
				elapsed = 0
			}
			appendTaskMessage(task, "info", fmt.Sprintf("总耗时（下载+导出）：%s", formatElapsedDuration(elapsed)))
		}
	})
}

func hasEPUBExport(exported []string) bool {
	for _, item := range exported {
		if strings.EqualFold(strings.TrimSpace(filepathExt(item)), ".epub") {
			return true
		}
	}
	return false
}

func filepathExt(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return ""
	}
	return path[idx:]
}

func formatElapsedDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d小时%d分%d秒", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%d分%d秒", m, s)
	}
	return fmt.Sprintf("%d秒", s)
}

func (s *DownloadTaskStore) MarkFailed(id string, err error) {
	if err == nil {
		return
	}
	s.update(id, func(task *DownloadTask) {
		now := time.Now().UTC()
		task.Status = "failed"
		task.Phase = "failed"
		task.Error = err.Error()
		task.FinishedAt = &now
		appendTaskMessage(task, "error", err.Error())
	})
}

func (s *DownloadTaskStore) update(id string, mutate func(task *DownloadTask)) {
	s.mu.Lock()
	task, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	mutate(task)
	task.UpdatedAt = time.Now().UTC()
	persister := s.persister
	snapshot := cloneTask(task)
	s.mu.Unlock()
	if persister != nil {
		_ = persister.Save(taskRecordFromSnapshot(snapshot))
	}
}

func appendTaskMessage(task *DownloadTask, level string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	task.Messages = append(task.Messages, DownloadTaskMessage{
		At:    time.Now().UTC(),
		Level: level,
		Text:  text,
	})
	if len(task.Messages) > maxTaskMessages {
		task.Messages = append([]DownloadTaskMessage(nil), task.Messages[len(task.Messages)-maxTaskMessages:]...)
	}
}

func cloneTask(task *DownloadTask) DownloadTask {
	if task == nil {
		return DownloadTask{}
	}
	cloned := *task
	if !isFiniteFloat(cloned.Speed) {
		cloned.Speed = 0
		cloned.ETA = ""
	}
	cloned.Messages = append([]DownloadTaskMessage(nil), task.Messages...)
	cloned.Exported = append([]string(nil), task.Exported...)
	cloned.Formats = append([]string(nil), task.Formats...)
	if task.FinishedAt != nil {
		finishedAt := *task.FinishedAt
		cloned.FinishedAt = &finishedAt
	}
	return cloned
}

func taskRecordFromSnapshot(task DownloadTask) config.DownloadTaskRecord {
	messages := make([]config.DownloadTaskMessageRecord, 0, len(task.Messages))
	for _, msg := range task.Messages {
		messages = append(messages, config.DownloadTaskMessageRecord{
			At:    msg.At,
			Level: msg.Level,
			Text:  msg.Text,
		})
	}
	return config.DownloadTaskRecord{
		ID:                task.ID,
		Site:              task.Site,
		BookID:            task.BookID,
		Title:             task.Title,
		Status:            task.Status,
		Phase:             task.Phase,
		Target:            task.Target,
		Formats:           append([]string(nil), task.Formats...),
		TotalChapters:     task.TotalChapters,
		CompletedChapters: task.CompletedChapters,
		CurrentChapter:    task.CurrentChapter,
		ETA:               task.ETA,
		Speed:             task.Speed,
		Exported:          append([]string(nil), task.Exported...),
		Messages:          messages,
		Error:             task.Error,
		StartTime:         task.StartTime,
		CreatedAt:         task.CreatedAt,
		UpdatedAt:         task.UpdatedAt,
		FinishedAt:        task.FinishedAt,
	}
}

func recordToTask(record config.DownloadTaskRecord) *DownloadTask {
	messages := make([]DownloadTaskMessage, 0, len(record.Messages))
	for _, msg := range record.Messages {
		messages = append(messages, DownloadTaskMessage{
			At:    msg.At,
			Level: msg.Level,
			Text:  msg.Text,
		})
	}
	return &DownloadTask{
		ID:                record.ID,
		Site:              record.Site,
		BookID:            record.BookID,
		Title:             record.Title,
		Status:            record.Status,
		Phase:             record.Phase,
		Target:            record.Target,
		Formats:           append([]string(nil), record.Formats...),
		TotalChapters:     record.TotalChapters,
		CompletedChapters: record.CompletedChapters,
		CurrentChapter:    record.CurrentChapter,
		ETA:               record.ETA,
		Speed:             record.Speed,
		Exported:          append([]string(nil), record.Exported...),
		Error:             record.Error,
		Messages:          messages,
		StartTime:         record.StartTime,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
		FinishedAt:        record.FinishedAt,
	}
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
