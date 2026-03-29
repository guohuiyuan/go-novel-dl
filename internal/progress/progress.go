package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type DownloadReporter interface {
	OnBookStart(site, bookID, title string, total int)
	OnBookProgress(done, total int, chapterTitle string)
	OnBookComplete(done, total int)
}

type NullReporter struct{}

func (NullReporter) OnBookStart(site, bookID, title string, total int)   {}
func (NullReporter) OnBookProgress(done, total int, chapterTitle string) {}
func (NullReporter) OnBookComplete(done, total int)                      {}

type ConsoleBar struct {
	out         io.Writer
	width       int
	mu          sync.Mutex
	activeTotal int
	active      bool
	startTime   time.Time
}

func NewConsoleBar(out io.Writer) *ConsoleBar {
	return &ConsoleBar{out: out, width: 24}
}

func (b *ConsoleBar) OnBookStart(site, bookID, title string, total int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.activeTotal = total
	b.active = false
	b.startTime = time.Now()
	if strings.TrimSpace(title) == "" {
		title = bookID
	}
	_, _ = fmt.Fprintf(b.out, "[PROGRESS] %s/%s %s (%d chapters)\n", site, bookID, title, total)
	if total == 0 {
		_, _ = fmt.Fprintf(b.out, "[PROGRESS] [------------------------] 0/0\n")
	}
}

func (b *ConsoleBar) OnBookProgress(done, total int, chapterTitle string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if total <= 0 {
		total = b.activeTotal
	}

	var etaStr string
	if done > 0 && !b.startTime.IsZero() {
		elapsed := time.Since(b.startTime)
		rate := float64(done) / elapsed.Seconds()
		if rate > 0 {
			remaining := total - done
			etaSeconds := float64(remaining) / rate
			etaStr = formatDuration(time.Duration(etaSeconds) * time.Second)
		}
	}

	bar := renderBar(done, total, b.width)
	msg := fmt.Sprintf("[PROGRESS] %s %d/%d", bar, done, total)
	if etaStr != "" {
		msg += " ETA:" + etaStr
	}
	if title := strings.TrimSpace(chapterTitle); title != "" {
		msg += " - " + title
	}
	if b.active {
		_, _ = fmt.Fprint(b.out, "\r")
	}
	_, _ = fmt.Fprintf(b.out, "%-150s", msg)
	b.active = true
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func (b *ConsoleBar) OnBookComplete(done, total int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if total <= 0 {
		total = b.activeTotal
	}
	bar := renderBar(total, total, b.width)
	if b.active {
		_, _ = fmt.Fprint(b.out, "\r")
	}
	_, _ = fmt.Fprintf(b.out, "[PROGRESS] %s %d/%d complete\n", bar, done, total)
	_, _ = fmt.Fprintln(b.out)
	b.active = false
}

func renderBar(done, total, width int) string {
	if width <= 0 {
		width = 24
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}
