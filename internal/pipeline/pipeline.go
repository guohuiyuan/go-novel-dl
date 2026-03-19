package pipeline

import (
	"strings"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type Runner struct{}

func New() *Runner {
	return &Runner{}
}

func (r *Runner) Run(book *model.Book, processors []config.ProcessorConfig) (*model.Book, string) {
	if book == nil {
		return nil, ""
	}

	current := book.Clone()
	stage := "raw"
	for _, processor := range processors {
		current = applyProcessor(current, processor)
		stage = processor.Name
	}
	current.UpdatedAt = time.Now().UTC()
	return current, stage
}

func applyProcessor(book *model.Book, processor config.ProcessorConfig) *model.Book {
	processed := book.Clone()
	if processed == nil {
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(processor.Name)) {
	case "", "raw":
		return processed
	case "cleaner":
		for idx, chapter := range processed.Chapters {
			content := strings.ReplaceAll(chapter.Content, "\r\n", "\n")
			content = strings.ReplaceAll(content, "\r", "\n")
			if processor.RemoveInvisible {
				content = strings.ReplaceAll(content, "\u200b", "")
				content = strings.ReplaceAll(content, "\ufeff", "")
			}
			processed.Chapters[idx].Content = strings.TrimSpace(content)
		}
	default:
		// Unknown processors are kept as no-ops in the initial scaffold.
	}

	return processed
}
