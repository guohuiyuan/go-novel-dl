package site

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

func TestNovalpieFullFlowUsesEncryptedChapterAPI(t *testing.T) {
	const token = "test-token"
	const sessionID = "test-session"
	const sessionKeyPlain = "reader-secret"
	const chapterID = "245640"

	sessionKey := base64.StdEncoding.EncodeToString([]byte(sessionKeyPlain))
	payload := encryptNovalpieTestPayload(t, sessionKeyPlain, `<p>First paragraph.</p><p>Second paragraph.</p>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/search":
			if got := r.URL.Query().Get("q"); got != "hunter" {
				t.Fatalf("unexpected search query: %q", got)
			}
			assertNovalpieAuth(t, r, token)
			writeNovalpieJSON(t, w, map[string]any{
				"success": true,
				"results": []map[string]any{{
					"id":          1059,
					"title":       "S Hunters",
					"author_name": "Author",
					"description": "Intro",
					"photo_url":   "/cover.jpg",
				}},
			})
		case "/api/novels/1059/detail":
			assertNovalpieAuth(t, r, token)
			writeNovalpieJSON(t, w, map[string]any{
				"success":     true,
				"id":          1059,
				"title":       "S Hunters",
				"author_name": "Author",
				"description": "Intro",
				"photo_url":   "/cover.jpg",
			})
		case "/api/novels/1059/chapters":
			assertNovalpieAuth(t, r, token)
			writeNovalpieJSON(t, w, map[string]any{
				"success": true,
				"data": []map[string]any{{
					"id":            245640,
					"chapterNumber": 1,
					"title":         "Magic Decline",
				}},
			})
		case "/api/reader/session-key":
			assertNovalpieAuth(t, r, token)
			if r.Header.Get("X-Client-Signature") == "" || r.Header.Get("X-Client-Timestamp") == "" || r.Header.Get("X-Client-Nonce") == "" {
				t.Fatalf("missing reader signature headers: %#v", r.Header)
			}
			writeNovalpieJSON(t, w, map[string]any{
				"success":     true,
				"session_id":  sessionID,
				"session_key": sessionKey,
				"expires":     time.Now().Add(time.Hour).Unix(),
			})
		case "/api/chapters/" + chapterID + "/content":
			assertNovalpieAuth(t, r, token)
			if got := r.URL.Query().Get("session"); got != sessionID {
				t.Fatalf("unexpected session: %q", got)
			}
			if got := r.URL.Query().Get("replace_mode"); got != "india" {
				t.Fatalf("unexpected replace_mode: %q", got)
			}
			if got := r.URL.Query().Get("show_images"); got != "1" {
				t.Fatalf("unexpected show_images: %q", got)
			}
			writeNovalpieJSON(t, w, map[string]any{
				"success":       true,
				"id":            245640,
				"novelId":       1059,
				"title":         "Magic Decline",
				"chapterNumber": 1,
				"content":       payload.content,
				"iv":            payload.iv,
				"tag":           payload.tag,
				"encrypted":     true,
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	cfg := config.DefaultConfig().ResolveSiteConfig("novalpie")
	cfg.Cookie = "Bearer " + token
	cfg.MirrorHosts = []string{server.URL}
	cfg.General.Output.IncludePicture = true
	site := NewNovalpieSite(cfg)

	results, err := site.Search(context.Background(), "hunter", 10)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "1059" || results[0].Title != "S Hunters" {
		t.Fatalf("unexpected search results: %+v", results)
	}
	if results[0].URL != server.URL+"/book/1059" {
		t.Fatalf("unexpected search result url: %q", results[0].URL)
	}

	book, err := site.DownloadPlan(context.Background(), model.BookRef{BookID: "1059"})
	if err != nil {
		t.Fatalf("DownloadPlan returned error: %v", err)
	}
	if book.Title != "S Hunters" || len(book.Chapters) != 1 || book.Chapters[0].ID != chapterID {
		t.Fatalf("unexpected book: %+v", book)
	}
	if book.SourceURL != server.URL+"/book/1059" {
		t.Fatalf("unexpected book source url: %q", book.SourceURL)
	}

	chapter, err := site.FetchChapter(context.Background(), book.ID, book.Chapters[0])
	if err != nil {
		t.Fatalf("FetchChapter returned error: %v", err)
	}
	if chapter.Title != "Magic Decline" {
		t.Fatalf("unexpected chapter title: %q", chapter.Title)
	}
	if chapter.Content != "First paragraph.\nSecond paragraph." || !chapter.Downloaded {
		t.Fatalf("unexpected chapter content: %+v", chapter)
	}
}

func TestNovalpieClientSignatureMatchesCurrentWebBundle(t *testing.T) {
	site := NewNovalpieSite(config.DefaultConfig().ResolveSiteConfig("novalpie"))
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/147.0.0.0 Safari/537.36 Edg/147.0.0.0"
	got := site.buildClientSignature(userAgent, "1700000000", "4fzzzxjy")
	if got != "W72iiAY1FLA1fFSfvve960mPGKZ=" {
		t.Fatalf("unexpected signature: %q", got)
	}
}

func TestNovalpieResolveURLAcceptsNovalpiaHost(t *testing.T) {
	site := NewNovalpieSite(config.DefaultConfig().ResolveSiteConfig("novalpie"))
	resolved, ok := site.ResolveURL("https://novalpia.cc/api/chapters/245640/content?session=abc")
	if !ok || resolved.ChapterID != "245640" {
		t.Fatalf("unexpected resolved url: %+v ok=%v", resolved, ok)
	}
}

type novalpieEncryptedTestPayload struct {
	content string
	iv      string
	tag     string
}

func encryptNovalpieTestPayload(t *testing.T, sessionKeyPlain, plain string) novalpieEncryptedTestPayload {
	t.Helper()
	keyHash := sha256.Sum256([]byte(sessionKeyPlain))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	iv := []byte("123456789012")
	sealed := gcm.Seal(nil, iv, []byte(plain), nil)
	tagSize := gcm.Overhead()
	content := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]
	return novalpieEncryptedTestPayload{
		content: base64.StdEncoding.EncodeToString(content),
		iv:      base64.StdEncoding.EncodeToString(iv),
		tag:     base64.StdEncoding.EncodeToString(tag),
	}
}

func assertNovalpieAuth(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("unexpected auth header for %s: %q", r.URL.Path, got)
	}
}

func writeNovalpieJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if _, err := w.Write(data); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("write response: %v", err)
	}
}
