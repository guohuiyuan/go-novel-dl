package web

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxTaskMessages = 200

type DownloadTaskMessage struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

type DownloadTask struct {
	ID                string                `json:"id"`
	Site              string                `json:"site"`
	BookID            string                `json:"book_id"`
	Title             string                `json:"title,omitempty"`
	Status            string                `json:"status"`
	Phase             string                `json:"phase"`
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
	lastProgressAt    time.Time             `json:"-"`
	smoothedRate      float64               `json:"-"`
}

type DownloadTaskStore struct {
	mu    sync.RWMutex
	seq   uint64
	tasks map[string]*DownloadTask
}

func NewDownloadTaskStore() *DownloadTaskStore {
	return &DownloadTaskStore{
		tasks: make(map[string]*DownloadTask),
	}
}

func (s *DownloadTaskStore) Create(siteKey string, bookID string) DownloadTask {
	now := time.Now().UTC()
	id := fmt.Sprintf("task-%d-%d", now.UnixNano(), atomic.AddUint64(&s.seq, 1))

	task := &DownloadTask{
		ID:        id,
		Site:      strings.TrimSpace(siteKey),
		BookID:    strings.TrimSpace(bookID),
		Status:    "queued",
		Phase:     "queued",
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
	s.mu.Unlock()

	return cloneTask(task)
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
		task.StartTime = now
		task.lastProgressAt = now
		task.smoothedRate = 0
		appendTaskMessage(task, "info", fmt.Sprintf("开始下载（%d章）", total))
	})
}

func (s *DownloadTaskStore) MarkLoadingChapters(id string, siteKey string, bookID string) {
	s.update(id, func(task *DownloadTask) {
		task.Status = "running"
		task.Phase = "loading_chapters"
		task.Site = siteKey
		task.BookID = bookID
		appendTaskMessage(task, "info", "正在加载章节列表...")
	})
}

func (s *DownloadTaskStore) MarkProgress(id string, done int, total int, chapterTitle string) {
	s.update(id, func(task *DownloadTask) {
		task.Status = "running"
		task.Phase = "downloading"
		previousDone := task.CompletedChapters
		task.CompletedChapters = done
		if total > 0 {
			task.TotalChapters = total
		}
		task.CurrentChapter = strings.TrimSpace(chapterTitle)

		now := time.Now().UTC()
		if done > 0 && !task.StartTime.IsZero() {
			deltaChapters := done - previousDone
			if deltaChapters > 0 {
				if !task.lastProgressAt.IsZero() {
					deltaSeconds := now.Sub(task.lastProgressAt).Seconds()
					if deltaSeconds > 1e-6 {
						instantRate := float64(deltaChapters) / deltaSeconds
						if instantRate > 0 && isFiniteFloat(instantRate) {
							if task.smoothedRate <= 0 || !isFiniteFloat(task.smoothedRate) {
								task.smoothedRate = instantRate
							} else {
								task.smoothedRate = 0.75*task.smoothedRate + 0.25*instantRate
							}
						}
					}
				}
				task.lastProgressAt = now
			}

			if task.smoothedRate > 0 && isFiniteFloat(task.smoothedRate) {
				remaining := task.TotalChapters - done
				etaSeconds := float64(remaining) / task.smoothedRate
				if etaSeconds < 0 || !isFiniteFloat(etaSeconds) {
					task.ETA = ""
				} else {
					task.ETA = formatETADuration(time.Duration(etaSeconds) * time.Second)
				}
				task.Speed = task.smoothedRate
			}
		}

		message := fmt.Sprintf("已下载章节 %d/%d", done, task.TotalChapters)
		if task.CurrentChapter != "" {
			message += ": " + task.CurrentChapter
		}
		appendTaskMessage(task, "progress", message)
	})
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
		task.CompletedChapters = done
		if total > 0 {
			task.TotalChapters = total
		}
		task.CurrentChapter = ""
		appendTaskMessage(task, "info", "章节抓取完成，正在导出")
	})
}

func (s *DownloadTaskStore) MarkCompleted(id string, title string, exported []string) {
	s.update(id, func(task *DownloadTask) {
		now := time.Now().UTC()
		task.Status = "completed"
		task.Phase = "completed"
		task.Error = ""
		task.FinishedAt = &now
		if strings.TrimSpace(title) != "" {
			task.Title = title
		}
		task.Exported = append([]string(nil), exported...)
		appendTaskMessage(task, "success", fmt.Sprintf("导出完成（%d个文件）", len(exported)))
	})
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
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return
	}
	mutate(task)
	task.UpdatedAt = time.Now().UTC()
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
	if task.FinishedAt != nil {
		finishedAt := *task.FinishedAt
		cloned.FinishedAt = &finishedAt
	}
	return cloned
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
