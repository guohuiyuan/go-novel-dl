package model

import "time"

type BookRef struct {
	BookID    string   `json:"book_id"`
	StartID   string   `json:"start_id,omitempty"`
	EndID     string   `json:"end_id,omitempty"`
	IgnoreIDs []string `json:"ignore_ids,omitempty"`
}

type Chapter struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	URL        string `json:"url,omitempty"`
	Volume     string `json:"volume,omitempty"`
	Order      int    `json:"order,omitempty"`
	Downloaded bool   `json:"downloaded,omitempty"`
}

type Book struct {
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
	Chapters     []Chapter `json:"chapters"`
}

type SearchResult struct {
	Site          string `json:"site"`
	BookID        string `json:"book_id"`
	Title         string `json:"title"`
	Author        string `json:"author"`
	Description   string `json:"description,omitempty"`
	URL           string `json:"url,omitempty"`
	LatestChapter string `json:"latest_chapter,omitempty"`
	CoverURL      string `json:"cover_url,omitempty"`
}

func (b *Book) Clone() *Book {
	if b == nil {
		return nil
	}

	clone := *b
	clone.Tags = cloneStrings(b.Tags)
	clone.Chapters = make([]Chapter, len(b.Chapters))
	copy(clone.Chapters, b.Chapters)
	return &clone
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}
