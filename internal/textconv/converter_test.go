package textconv

import (
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestNormalizeBookLocaleToSimplified(t *testing.T) {
	book := &model.Book{
		Title:       "一擊史萊姆",
		Author:      "測試作者",
		Description: "這裡有龍與貓。",
		Tags:        []string{"異世界", "搞笑"},
		Chapters: []model.Chapter{{
			Title:   "第十章",
			Content: "這裡是會長與冒險者。",
			Volume:  "第一卷",
		}},
	}

	converted := NormalizeBookLocale(book, "simplified")
	if converted.Title != "一击史莱姆" {
		t.Fatalf("unexpected title: %s", converted.Title)
	}
	if converted.Chapters[0].Content != "这里是会长与冒险者。" {
		t.Fatalf("unexpected chapter content: %s", converted.Chapters[0].Content)
	}
	if book.Title != "一擊史萊姆" {
		t.Fatalf("original book should remain unchanged")
	}
}

func TestNormalizeSearchResultsLocale(t *testing.T) {
	results := []model.SearchResult{{Title: "一擊史萊姆", Author: "測試作者", LatestChapter: "第十章"}}
	converted := NormalizeSearchResultsLocale(results, "simplified")
	if converted[0].Title != "一击史莱姆" {
		t.Fatalf("unexpected search title: %s", converted[0].Title)
	}
	if results[0].Title != "一擊史萊姆" {
		t.Fatalf("original results should remain unchanged")
	}
}
