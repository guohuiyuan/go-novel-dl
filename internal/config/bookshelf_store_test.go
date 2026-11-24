package config

import (
	"testing"
	"time"
)

func TestBookshelfFolderAndBookCRUD(t *testing.T) {
	resetSiteCatalogForTest(t)

	folder, err := CreateBookshelfFolder(nil, "  收藏夹  ")
	if err != nil {
		t.Fatalf("create root folder: %v", err)
	}
	if folder.ID == 0 {
		t.Fatalf("expected folder id, got %+v", folder)
	}
	if folder.Name != "收藏夹" {
		t.Fatalf("expected trimmed folder name, got %q", folder.Name)
	}
	if folder.Kind != BookshelfItemKindFolder {
		t.Fatalf("expected folder kind, got %q", folder.Kind)
	}

	if _, err := CreateBookshelfFolder(nil, "   "); err == nil {
		t.Fatalf("expected empty folder name to be rejected")
	}

	book, err := AddBookshelfBook(BookshelfBookInput{
		ParentID: &folder.ID,
		Site:     "esjzone",
		BookID:   "1755960125",
		Title:    "无题",
		Author:   "作者",
	})
	if err != nil {
		t.Fatalf("add book: %v", err)
	}
	if book.ParentID == nil || *book.ParentID != folder.ID {
		t.Fatalf("expected book to be in folder, got %+v", book)
	}

	// Re-adding the same site/book should reuse the row but apply latest metadata.
	again, err := AddBookshelfBook(BookshelfBookInput{
		Site:          "esjzone",
		BookID:        "1755960125",
		Title:         "新标题",
		LatestChapter: "第99章",
	})
	if err != nil {
		t.Fatalf("re-add book: %v", err)
	}
	if again.ID != book.ID {
		t.Fatalf("expected same book row, got id=%d want=%d", again.ID, book.ID)
	}
	if again.LatestChapter != "第99章" {
		t.Fatalf("expected latest chapter to be updated, got %q", again.LatestChapter)
	}

	rootItems, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list root: %v", err)
	}
	if len(rootItems) != 1 || rootItems[0].ID != folder.ID {
		t.Fatalf("expected single root folder, got %+v", rootItems)
	}
	if rootItems[0].ChildCount != 1 {
		t.Fatalf("expected folder child_count=1, got %d", rootItems[0].ChildCount)
	}

	childItems, err := ListBookshelfItems(&folder.ID)
	if err != nil {
		t.Fatalf("list folder children: %v", err)
	}
	if len(childItems) != 1 || childItems[0].Kind != BookshelfItemKindBook {
		t.Fatalf("expected one book inside folder, got %+v", childItems)
	}

	rename := "我的书架"
	updated, err := UpdateBookshelfItem(folder.ID, BookshelfMutation{Name: &rename})
	if err != nil {
		t.Fatalf("rename folder: %v", err)
	}
	if updated.Name != rename {
		t.Fatalf("expected rename to apply, got %q", updated.Name)
	}

	// Moving the book back to root.
	if _, err := UpdateBookshelfItem(book.ID, BookshelfMutation{ClearParent: true}); err != nil {
		t.Fatalf("move book to root: %v", err)
	}
	moved, err := GetBookshelfItem(book.ID)
	if err != nil {
		t.Fatalf("reload moved book: %v", err)
	}
	if moved.ParentID != nil {
		t.Fatalf("expected book at root, got parent=%v", moved.ParentID)
	}

	// Updating cache stats should persist.
	if err := UpdateBookshelfCacheStats("esjzone", "1755960125", 12, 5); err != nil {
		t.Fatalf("update cache stats: %v", err)
	}
	cached, err := GetBookshelfItem(book.ID)
	if err != nil {
		t.Fatalf("reload book after stats: %v", err)
	}
	if cached.TotalChapters != 12 || cached.CachedChapters != 5 {
		t.Fatalf("expected stats persisted, got total=%d cached=%d", cached.TotalChapters, cached.CachedChapters)
	}

	if _, err := CreateBookshelfFolder(&folder.ID, "子目录"); err == nil {
		t.Fatalf("expected nested folder creation to be rejected")
	}

	folderBook, err := AddBookshelfBook(BookshelfBookInput{
		ParentID: &folder.ID,
		Site:     "esjzone",
		BookID:   "another",
		Title:    "Another",
	})
	if err != nil {
		t.Fatalf("add book to folder: %v", err)
	}
	if err := DeleteBookshelfItem(folder.ID); err != nil {
		t.Fatalf("delete folder cascade: %v", err)
	}
	all, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list root after delete: %v", err)
	}
	if len(all) != 1 || all[0].ID != book.ID {
		t.Fatalf("expected only the rooted book to survive cascade, got %+v", all)
	}
	if _, err := GetBookshelfItem(folderBook.ID); err == nil {
		t.Fatalf("expected folder book to be cascade-deleted")
	}
}

func TestBookshelfSingleLevelFolderValidation(t *testing.T) {
	resetSiteCatalogForTest(t)

	root, err := CreateBookshelfFolder(nil, "root")
	if err != nil {
		t.Fatalf("create root folder: %v", err)
	}
	another, err := CreateBookshelfFolder(nil, "another")
	if err != nil {
		t.Fatalf("create another folder: %v", err)
	}

	breadcrumb, err := BookshelfBreadcrumb(root.ID)
	if err != nil {
		t.Fatalf("breadcrumb: %v", err)
	}
	if len(breadcrumb) != 1 || breadcrumb[0].ID != root.ID {
		t.Fatalf("expected single root breadcrumb, got %+v", breadcrumb)
	}

	if _, err := CreateBookshelfFolder(&root.ID, "child"); err == nil {
		t.Fatalf("expected creating nested folder to be rejected")
	}

	if _, err := UpdateBookshelfItem(another.ID, BookshelfMutation{ParentID: &root.ID}); err == nil {
		t.Fatalf("expected moving folder into another folder to be rejected")
	}

	book, err := AddBookshelfBook(BookshelfBookInput{
		ParentID: &root.ID,
		Site:     "esjzone",
		BookID:   "abc",
		Title:    "Book",
	})
	if err != nil {
		t.Fatalf("create book: %v", err)
	}
	if _, err := UpdateBookshelfItem(another.ID, BookshelfMutation{ParentID: &book.ID}); err == nil {
		t.Fatalf("expected moving folder into a book to be rejected")
	}
}

func TestBookshelfReadingProgressAndHistory(t *testing.T) {
	resetSiteCatalogForTest(t)

	bookA, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "alpha", Title: "Alpha"})
	if err != nil {
		t.Fatalf("add book A: %v", err)
	}
	bookB, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "beta", Title: "Beta"})
	if err != nil {
		t.Fatalf("add book B: %v", err)
	}

	// Updating progress for an unknown book materialises an implicit history
	// row so the reading-history feed can pick it up even when the book has
	// never been added to the shelf.
	implicit, found, err := UpdateBookshelfProgress(BookshelfProgressInput{
		Site:         "esjzone",
		BookID:       "missing",
		ChapterID:    "ch-1",
		ChapterIndex: 0,
		ChapterTitle: "第一章",
		Page:         6,
		Title:        "Missing Book",
		Author:       "Anon",
	})
	if err != nil {
		t.Fatalf("progress for missing book should not error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false when creating an implicit row")
	}
	if !implicit.Implicit {
		t.Fatalf("expected the new row to be marked implicit, got %+v", implicit)
	}
	if implicit.Title != "Missing Book" || implicit.Author != "Anon" {
		t.Fatalf("implicit row should carry submitted metadata, got %+v", implicit)
	}
	if implicit.LastReadPage != 6 {
		t.Fatalf("expected implicit row page 6, got %+v", implicit)
	}

	// Implicit rows must stay out of bookshelf listings.
	rootItems, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list root after implicit progress: %v", err)
	}
	for _, item := range rootItems {
		if item.ID == implicit.ID {
			t.Fatalf("implicit row leaked into bookshelf listing: %+v", item)
		}
	}

	itemA, found, err := UpdateBookshelfProgress(BookshelfProgressInput{
		Site:         "esjzone",
		BookID:       "alpha",
		ChapterID:    "ch-7",
		ChapterIndex: 6,
		ChapterTitle: " 第七章 ",
		Page:         4,
	})
	if err != nil || !found {
		t.Fatalf("update progress A: err=%v found=%v", err, found)
	}
	if itemA.ID != bookA.ID {
		t.Fatalf("expected progress to update book A, got id=%d", itemA.ID)
	}
	if itemA.LastReadChapterID != "ch-7" || itemA.LastReadChapterIndex != 6 {
		t.Fatalf("unexpected progress: %+v", itemA)
	}
	if itemA.LastReadChapterTitle != "第七章" {
		t.Fatalf("expected trimmed chapter title, got %q", itemA.LastReadChapterTitle)
	}
	if itemA.LastReadPage != 4 {
		t.Fatalf("expected page 4, got %+v", itemA)
	}
	if itemA.LastReadAt == nil {
		t.Fatalf("expected last_read_at to be set")
	}

	// Make book B more recent.
	time.Sleep(10 * time.Millisecond)
	if _, _, err := UpdateBookshelfProgress(BookshelfProgressInput{
		Site:         "esjzone",
		BookID:       "beta",
		ChapterID:    "b-3",
		ChapterIndex: 2,
		ChapterTitle: "卷一·第三章",
	}); err != nil {
		t.Fatalf("update progress B: %v", err)
	}

	history, err := ListBookshelfHistory(0)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	// Expect three entries: implicit row, book A, book B (most-recent first).
	if len(history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(history))
	}
	if history[0].ID != bookB.ID {
		t.Fatalf("expected most recent first; head=%+v", history[0])
	}
	if history[1].ID != bookA.ID {
		t.Fatalf("expected book A second; got %+v", history[1])
	}
	if history[2].ID != implicit.ID || !history[2].Implicit {
		t.Fatalf("expected implicit row last; got %+v", history[2])
	}

	// Limit must cap the result set.
	limited, err := ListBookshelfHistory(1)
	if err != nil {
		t.Fatalf("list history limited: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != bookB.ID {
		t.Fatalf("expected single most-recent entry, got %+v", limited)
	}

	// Promoting an implicit row by adding it to the shelf should clear the
	// implicit flag and keep the existing progress intact.
	promoted, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "missing", Title: "Missing Book"})
	if err != nil {
		t.Fatalf("promote implicit row: %v", err)
	}
	if promoted.ID != implicit.ID {
		t.Fatalf("expected promotion to reuse the implicit row, got id=%d", promoted.ID)
	}
	if promoted.Implicit {
		t.Fatalf("expected implicit flag to be cleared after promotion: %+v", promoted)
	}
	if promoted.LastReadChapterID != "ch-1" {
		t.Fatalf("expected reading progress preserved on promotion: %+v", promoted)
	}

	// Books without progress should not appear in history.
	silent, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "silent", Title: "Silent"})
	if err != nil {
		t.Fatalf("add silent book: %v", err)
	}
	historyAgain, err := ListBookshelfHistory(0)
	if err != nil {
		t.Fatalf("list history again: %v", err)
	}
	for _, item := range historyAgain {
		if item.ID == silent.ID {
			t.Fatalf("expected silent book to be excluded from history")
		}
	}

	// Removing a book that already has reading progress should demote the row
	// to implicit history rather than wiping it.
	if err := DeleteBookshelfItem(bookB.ID); err != nil {
		t.Fatalf("delete book with progress: %v", err)
	}
	demoted, err := GetBookshelfItem(bookB.ID)
	if err != nil {
		t.Fatalf("expected demoted row to remain accessible: %v", err)
	}
	if !demoted.Implicit {
		t.Fatalf("expected demoted row to be marked implicit, got %+v", demoted)
	}
	rootAfterRemove, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list root after demote: %v", err)
	}
	for _, item := range rootAfterRemove {
		if item.ID == bookB.ID {
			t.Fatalf("demoted row leaked back into bookshelf listing: %+v", item)
		}
	}

	// A book without any progress should still be deleted outright.
	if err := DeleteBookshelfItem(silent.ID); err != nil {
		t.Fatalf("delete silent book: %v", err)
	}
	if _, err := GetBookshelfItem(silent.ID); err == nil {
		t.Fatalf("expected silent book to be deleted")
	}
}

func TestFindBookshelfProgress(t *testing.T) {
	resetSiteCatalogForTest(t)

	if _, ok, err := FindBookshelfProgress("", "whatever"); err != nil || ok {
		t.Fatalf("expected empty site to be a miss without error, got ok=%v err=%v", ok, err)
	}
	if _, ok, err := FindBookshelfProgress("esjzone", "  "); err != nil || ok {
		t.Fatalf("expected empty book_id to be a miss without error, got ok=%v err=%v", ok, err)
	}

	if _, ok, err := FindBookshelfProgress("esjzone", "unknown"); err != nil || ok {
		t.Fatalf("expected unknown book to be a miss, got ok=%v err=%v", ok, err)
	}

	shelved, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "alpha", Title: "Alpha"})
	if err != nil {
		t.Fatalf("add book: %v", err)
	}
	found, ok, err := FindBookshelfProgress(" esjzone ", " alpha ")
	if err != nil || !ok {
		t.Fatalf("expected shelf book to be found, ok=%v err=%v", ok, err)
	}
	if found.ID != shelved.ID || found.Implicit {
		t.Fatalf("unexpected shelf lookup result: %+v", found)
	}

	implicit, _, err := UpdateBookshelfProgress(BookshelfProgressInput{
		Site:         "esjzone",
		BookID:       "history-only",
		ChapterID:    "ch-2",
		ChapterIndex: 1,
		ChapterTitle: "第二章",
		Page:         3,
		Title:        "History Only",
	})
	if err != nil {
		t.Fatalf("create implicit history row: %v", err)
	}
	found, ok, err = FindBookshelfProgress("esjzone", "history-only")
	if err != nil || !ok {
		t.Fatalf("expected implicit row to be found, ok=%v err=%v", ok, err)
	}
	if found.ID != implicit.ID || !found.Implicit {
		t.Fatalf("unexpected implicit lookup result: %+v", found)
	}
	if found.LastReadChapterID != "ch-2" || found.LastReadChapterIndex != 1 {
		t.Fatalf("expected reading progress on lookup, got %+v", found)
	}
	if found.LastReadPage != 3 {
		t.Fatalf("expected page progress on lookup, got %+v", found)
	}
}

func TestBookshelfOrdersRecentlyUsedFirstAndPaginates(t *testing.T) {
	resetSiteCatalogForTest(t)

	alpha, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "alpha", Title: "Alpha"})
	if err != nil {
		t.Fatalf("add alpha: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	beta, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "beta", Title: "Beta"})
	if err != nil {
		t.Fatalf("add beta: %v", err)
	}

	items, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list shelf: %v", err)
	}
	if len(items) != 2 || items[0].ID != beta.ID {
		t.Fatalf("newly added book should lead the shelf, got %+v", items)
	}

	time.Sleep(5 * time.Millisecond)
	if _, err := TouchBookshelfItem(alpha.ID); err != nil {
		t.Fatalf("touch alpha: %v", err)
	}
	items, err = ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list shelf after touch: %v", err)
	}
	if items[0].ID != alpha.ID {
		t.Fatalf("clicked book should move to the front, got %+v", items)
	}

	time.Sleep(5 * time.Millisecond)
	if _, _, err := UpdateBookshelfProgress(BookshelfProgressInput{
		Site:         "esjzone",
		BookID:       "beta",
		ChapterID:    "ch-1",
		ChapterIndex: 0,
		ChapterTitle: "第一章",
	}); err != nil {
		t.Fatalf("progress beta: %v", err)
	}
	items, err = ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list shelf after progress: %v", err)
	}
	if items[0].ID != beta.ID {
		t.Fatalf("most recently read book should lead the shelf, got %+v", items)
	}

	pageItems, total, err := ListBookshelfItemsPage(nil, 1, 1)
	if err != nil {
		t.Fatalf("list shelf page: %v", err)
	}
	if total != 2 || len(pageItems) != 1 || pageItems[0].ID != alpha.ID {
		t.Fatalf("unexpected shelf page: total=%d items=%+v", total, pageItems)
	}
	if count, err := CountBookshelfBooks(nil); err != nil || count != 2 {
		t.Fatalf("unexpected book count: count=%d err=%v", count, err)
	}
}

func TestBookshelfHistoryPagination(t *testing.T) {
	resetSiteCatalogForTest(t)

	for _, id := range []string{"a", "b", "c", "d", "e"} {
		if _, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: id, Title: id}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
		if _, _, err := UpdateBookshelfProgress(BookshelfProgressInput{
			Site:         "esjzone",
			BookID:       id,
			ChapterID:    "ch-" + id,
			ChapterIndex: 0,
			ChapterTitle: "第 " + id + " 章",
		}); err != nil {
			t.Fatalf("progress %s: %v", id, err)
		}
		time.Sleep(3 * time.Millisecond)
	}

	items, total, err := ListBookshelfHistoryPage(2, 2)
	if err != nil {
		t.Fatalf("history page: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected history total 5, got %d", total)
	}
	if len(items) != 2 || items[0].BookID != "c" || items[1].BookID != "b" {
		t.Fatalf("unexpected history page window: %+v", items)
	}
}

func TestBookshelfKeepsFoldersAheadOfBooks(t *testing.T) {
	resetSiteCatalogForTest(t)

	if _, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "alpha", Title: "Alpha"}); err != nil {
		t.Fatalf("add book: %v", err)
	}
	folder, err := CreateBookshelfFolder(nil, "Favorites")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	items, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list shelf: %v", err)
	}
	if len(items) != 2 || items[0].Kind != BookshelfItemKindFolder || items[0].ID != folder.ID {
		t.Fatalf("folders must stay ahead of books, got %+v", items)
	}
}

func TestBookshelfSortsLegacyRowsByRealTimestamp(t *testing.T) {
	resetSiteCatalogForTest(t)

	legacy, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "legacy", Title: "Legacy"})
	if err != nil {
		t.Fatalf("add legacy book: %v", err)
	}
	db, err := bookshelfDB()
	if err != nil {
		t.Fatalf("open bookshelf db: %v", err)
	}
	// Simulate a row written before LastUsedAt existed, with a local-time
	// AddedAt that would sort incorrectly if compared as raw text.
	if err := db.Model(&bookshelfItem{}).Where("id = ?", legacy.ID).Updates(map[string]any{
		"last_used_at": nil,
		"added_at":     time.Now().Add(-2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("prepare legacy row: %v", err)
	}
	newer, err := AddBookshelfBook(BookshelfBookInput{Site: "esjzone", BookID: "newer", Title: "Newer"})
	if err != nil {
		t.Fatalf("add newer book: %v", err)
	}
	items, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list shelf: %v", err)
	}
	if len(items) != 2 || items[0].ID != newer.ID {
		t.Fatalf("expected the newer book first, got %+v", items)
	}
}
