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

	// Folder delete cascades to children.
	subfolder, err := CreateBookshelfFolder(&folder.ID, "子目录")
	if err != nil {
		t.Fatalf("create subfolder: %v", err)
	}
	if _, err := AddBookshelfBook(BookshelfBookInput{
		ParentID: &subfolder.ID,
		Site:     "esjzone",
		BookID:   "another",
		Title:    "Another",
	}); err != nil {
		t.Fatalf("add book to subfolder: %v", err)
	}
	if err := DeleteBookshelfItem(folder.ID); err != nil {
		t.Fatalf("delete folder cascade: %v", err)
	}
	all, err := ListBookshelfItems(nil)
	if err != nil {
		t.Fatalf("list root after delete: %v", err)
	}
	// The originally-rooted book remains, the deleted folder and subfolder + their book are gone.
	if len(all) != 1 || all[0].ID != book.ID {
		t.Fatalf("expected only the rooted book to survive cascade, got %+v", all)
	}
	if _, err := GetBookshelfItem(subfolder.ID); err == nil {
		t.Fatalf("expected subfolder to be cascade-deleted")
	}
}

func TestBookshelfBreadcrumbAndMoveValidation(t *testing.T) {
	resetSiteCatalogForTest(t)

	root, err := CreateBookshelfFolder(nil, "root")
	if err != nil {
		t.Fatalf("create root folder: %v", err)
	}
	child, err := CreateBookshelfFolder(&root.ID, "child")
	if err != nil {
		t.Fatalf("create child folder: %v", err)
	}
	leaf, err := CreateBookshelfFolder(&child.ID, "leaf")
	if err != nil {
		t.Fatalf("create leaf folder: %v", err)
	}

	breadcrumb, err := BookshelfBreadcrumb(leaf.ID)
	if err != nil {
		t.Fatalf("breadcrumb: %v", err)
	}
	if len(breadcrumb) != 3 || breadcrumb[0].ID != root.ID || breadcrumb[2].ID != leaf.ID {
		t.Fatalf("expected breadcrumb root->child->leaf, got %+v", breadcrumb)
	}

	// Moving root into its descendant should fail.
	if _, err := UpdateBookshelfItem(root.ID, BookshelfMutation{ParentID: &leaf.ID}); err == nil {
		t.Fatalf("expected moving folder into its descendant to be rejected")
	}
	// Moving folder into itself should also fail.
	if _, err := UpdateBookshelfItem(child.ID, BookshelfMutation{ParentID: &child.ID}); err == nil {
		t.Fatalf("expected moving folder into itself to be rejected")
	}
	// Moving a folder into a book is not allowed (parent must be folder).
	book, err := AddBookshelfBook(BookshelfBookInput{
		ParentID: &root.ID,
		Site:     "esjzone",
		BookID:   "abc",
		Title:    "Book",
	})
	if err != nil {
		t.Fatalf("create book: %v", err)
	}
	if _, err := UpdateBookshelfItem(child.ID, BookshelfMutation{ParentID: &book.ID}); err == nil {
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

	// Updating progress for an unknown book is a silent no-op.
	if _, found, err := UpdateBookshelfProgress(BookshelfProgressInput{Site: "esjzone", BookID: "missing", ChapterID: "1"}); err != nil {
		t.Fatalf("progress for missing book should not error: %v", err)
	} else if found {
		t.Fatalf("expected found=false for missing book")
	}

	itemA, found, err := UpdateBookshelfProgress(BookshelfProgressInput{
		Site:         "esjzone",
		BookID:       "alpha",
		ChapterID:    "ch-7",
		ChapterIndex: 6,
		ChapterTitle: " 第七章 ",
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
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].ID != bookB.ID {
		t.Fatalf("expected most recent first; head=%+v", history[0])
	}
	if history[1].ID != bookA.ID {
		t.Fatalf("expected book A second; got %+v", history[1])
	}

	// Limit must cap the result set.
	limited, err := ListBookshelfHistory(1)
	if err != nil {
		t.Fatalf("list history limited: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != bookB.ID {
		t.Fatalf("expected single most-recent entry, got %+v", limited)
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
}
