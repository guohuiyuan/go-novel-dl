package textconv

import (
	"strings"

	opencc "github.com/liuzl/gocc"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var t2s, t2sErr = opencc.New("t2s")

func NormalizeBookLocale(book *model.Book, localeStyle string) *model.Book {
	if book == nil {
		return nil
	}
	style := strings.ToLower(strings.TrimSpace(localeStyle))
	if style == "" || style == "original" || style == "traditional" {
		return book
	}
	if style != "simplified" && style != "zh_cn" && style != "zh-cn" && style != "zh-hans" {
		return book
	}

	converted := book.Clone()
	converted.Title = ToSimplified(converted.Title)
	converted.Author = ToSimplified(converted.Author)
	converted.Description = ToSimplified(converted.Description)
	for idx, tag := range converted.Tags {
		converted.Tags[idx] = ToSimplified(tag)
	}
	for idx, chapter := range converted.Chapters {
		converted.Chapters[idx].Title = ToSimplified(chapter.Title)
		converted.Chapters[idx].Content = ToSimplified(chapter.Content)
		converted.Chapters[idx].Volume = ToSimplified(chapter.Volume)
	}
	return converted
}

func ToSimplified(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	if t2sErr != nil || t2s == nil {
		return value
	}
	converted, err := t2s.Convert(value)
	if err != nil {
		return value
	}
	return converted
}

func NormalizeSearchResultsLocale(results []model.SearchResult, localeStyle string) []model.SearchResult {
	style := strings.ToLower(strings.TrimSpace(localeStyle))
	if style == "" || style == "original" || style == "traditional" {
		return results
	}
	if style != "simplified" && style != "zh_cn" && style != "zh-cn" && style != "zh-hans" {
		return results
	}

	converted := make([]model.SearchResult, len(results))
	copy(converted, results)
	for idx, result := range converted {
		converted[idx].Title = ToSimplified(result.Title)
		converted[idx].Author = ToSimplified(result.Author)
		converted[idx].Description = ToSimplified(result.Description)
		converted[idx].LatestChapter = ToSimplified(result.LatestChapter)
	}
	return converted
}
