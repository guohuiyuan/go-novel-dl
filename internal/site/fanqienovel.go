package site

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

var (
	fanqieBookRe         = regexp.MustCompile(`^/page/(\d+)/?$`)
	fanqieChapterRe      = regexp.MustCompile(`^/reader/(\d+)/?$`)
	fanqieInitialStateRe = regexp.MustCompile(`window\.__INITIAL_STATE__\s*=\s*({.*});`)
)

//go:embed resources/fanqienovel.json
var fanqieMapRaw string

var fanqieMap = mustLoadSubstMap(fanqieMapRaw)

type FanqieNovelSite struct {
	cfg  config.ResolvedSiteConfig
	html HTMLSite
}

func NewFanqieNovelSite(cfg config.ResolvedSiteConfig) *FanqieNovelSite {
	timeout := 15 * time.Second
	if cfg.General.Timeout > 0 {
		timeout = time.Duration(cfg.General.Timeout * float64(time.Second))
	}
	return &FanqieNovelSite{cfg: cfg, html: NewHTMLSite(&http.Client{Timeout: timeout})}
}

func (s *FanqieNovelSite) Key() string         { return "fanqienovel" }
func (s *FanqieNovelSite) DisplayName() string { return "FanqieNovel" }
func (s *FanqieNovelSite) Capabilities() Capabilities {
	return Capabilities{Download: true, Search: false, Login: false}
}

func (s *FanqieNovelSite) ResolveURL(rawURL string) (*ResolvedURL, bool) {
	parsed, err := normalizeURL(rawURL)
	if err != nil {
		return nil, false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	if host != "fanqienovel.com" {
		return nil, false
	}
	if m := fanqieChapterRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), ChapterID: m[1], Canonical: "https://fanqienovel.com" + parsed.Path}, true
	}
	if m := fanqieBookRe.FindStringSubmatch(parsed.Path); len(m) == 2 {
		return &ResolvedURL{SiteKey: s.Key(), BookID: m[1], Canonical: "https://fanqienovel.com" + parsed.Path}, true
	}
	return nil, false
}

func (s *FanqieNovelSite) Download(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	book, err := s.DownloadPlan(ctx, ref)
	if err != nil {
		return nil, err
	}
	for idx, chapter := range book.Chapters {
		loaded, err := s.FetchChapter(ctx, ref.BookID, chapter)
		if err != nil {
			return nil, err
		}
		loaded.Order = idx + 1
		book.Chapters[idx] = loaded
	}
	return book, nil
}

func (s *FanqieNovelSite) DownloadPlan(ctx context.Context, ref model.BookRef) (*model.Book, error) {
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://fanqienovel.com/page/%s", ref.BookID))
	if err != nil {
		return nil, err
	}
	state, err := extractFanqieInitialState(markup)
	if err != nil {
		return nil, err
	}
	page := mapValue(state, "page")
	if page == nil {
		return nil, fmt.Errorf("fanqienovel page state not found")
	}
	book := &model.Book{
		Site:         s.Key(),
		ID:           ref.BookID,
		Title:        stringValue(page["bookName"]),
		Author:       fallback(stringValue(page["authorName"]), stringValue(page["author"])),
		Description:  fallback(stringValue(page["abstract"]), stringValue(page["description"])),
		SourceURL:    fmt.Sprintf("https://fanqienovel.com/page/%s", ref.BookID),
		CoverURL:     fallback(stringValue(page["thumbUrl"]), stringValue(page["thumbUri"])),
		DownloadedAt: time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	book.Tags = fanqieCategoryTags(stringValue(page["categoryV2"]))
	volumeNames := stringSliceValue(page["volumeNameList"])
	chapterGroups := sliceValue(page["chapterListWithVolume"])
	chapters := make([]model.Chapter, 0)
	for i, group := range chapterGroups {
		items := sliceValue(group)
		if len(items) == 0 {
			continue
		}
		volumeName := fmt.Sprintf("卷 %d", i+1)
		if i < len(volumeNames) && strings.TrimSpace(volumeNames[i]) != "" {
			volumeName = volumeNames[i]
		}
		mapped := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m := mapValue(item); m != nil {
				mapped = append(mapped, m)
			}
		}
		sort.SliceStable(mapped, func(i, j int) bool {
			return fanqieSortKey(mapped[i]) < fanqieSortKey(mapped[j])
		})
		for _, item := range mapped {
			cid := stringValue(item["itemId"])
			if cid == "" {
				continue
			}
			locked := boolValue(item["isChapterLock"])
			if locked && !s.cfg.General.FetchInaccessible {
				continue
			}
			chapters = append(chapters, model.Chapter{
				ID:     cid,
				Title:  stringValue(item["title"]),
				URL:    fmt.Sprintf("https://fanqienovel.com/reader/%s", cid),
				Volume: volumeName,
				Order:  len(chapters) + 1,
			})
		}
	}
	book.Chapters = applyChapterRange(chapters, ref)
	return book, nil
}

func (s *FanqieNovelSite) FetchChapter(ctx context.Context, bookID string, chapter model.Chapter) (model.Chapter, error) {
	_ = bookID
	markup, err := s.html.Get(ctx, fmt.Sprintf("https://fanqienovel.com/reader/%s", chapter.ID))
	if err != nil {
		return chapter, err
	}
	state, err := extractFanqieInitialState(markup)
	if err != nil {
		return chapter, err
	}
	reader := mapValue(state, "reader")
	if reader == nil {
		return chapter, fmt.Errorf("fanqienovel reader state not found")
	}
	chapterData := mapValue(reader, "chapterData")
	if chapterData == nil {
		return chapter, fmt.Errorf("fanqienovel chapterData not found")
	}
	rawContent := stringValue(chapterData["content"])
	if strings.TrimSpace(rawContent) == "" {
		return chapter, fmt.Errorf("fanqienovel chapter content not found")
	}
	doc, err := parseHTML(rawContent)
	if err != nil {
		return chapter, err
	}
	paragraphs := make([]string, 0)
	for _, p := range findAll(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "p" }) {
		text := applySubstMap(strings.TrimSpace(nodeText(p)), fanqieMap)
		text = cleanText(text)
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	}
	if len(paragraphs) == 0 {
		return chapter, fmt.Errorf("fanqienovel parsed paragraph content is empty")
	}
	if title := stringValue(chapterData["title"]); title != "" {
		chapter.Title = title
	}
	chapter.Content = strings.Join(paragraphs, "\n")
	chapter.Downloaded = true
	return chapter, nil
}

func (s *FanqieNovelSite) Search(ctx context.Context, keyword string, limit int) ([]model.SearchResult, error) {
	_ = ctx
	_ = keyword
	_ = limit
	return nil, fmt.Errorf("fanqienovel search is not implemented yet")
}

func extractFanqieInitialState(markup string) (map[string]any, error) {
	match := fanqieInitialStateRe.FindStringSubmatch(markup)
	if len(match) != 2 {
		return nil, fmt.Errorf("fanqienovel initial state not found")
	}
	tokens := tokenizeJSObject(match[1])
	value, next, err := parseJSValue(tokens, 0)
	if err != nil {
		return nil, err
	}
	if next == 0 {
		return nil, fmt.Errorf("fanqienovel initial state parser consumed nothing")
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("fanqienovel initial state is not an object")
	}
	return result, nil
}

func tokenizeJSObject(src string) []string {
	toks := make([]string, 0)
	for i := 0; i < len(src); {
		ch := src[i]
		if strings.ContainsRune(" \t\r\n", rune(ch)) {
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			j := i + 1
			esc := false
			for j < len(src) {
				c := src[j]
				if esc {
					esc = false
				} else if c == '\\' {
					esc = true
				} else if c == ch {
					j++
					break
				}
				j++
			}
			toks = append(toks, src[i:j])
			i = j
			continue
		}
		if ch == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
			if src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' && src[i] != '\r' {
					i++
				}
			} else {
				i += 2
				for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
					i++
				}
				i += 2
			}
			continue
		}
		if strings.ContainsRune("{}[]:,", rune(ch)) {
			toks = append(toks, src[i:i+1])
			i++
			continue
		}
		j := i
		for j < len(src) && !strings.ContainsRune(" \t\r\n{}[]:,", rune(src[j])) {
			j++
		}
		toks = append(toks, src[i:j])
		i = j
	}
	return toks
}

func parseJSValue(tokens []string, idx int) (any, int, error) {
	if idx >= len(tokens) {
		return nil, idx, fmt.Errorf("unexpected end of tokens")
	}
	tok := tokens[idx]
	if tok == "{" {
		obj := map[string]any{}
		idx++
		for idx < len(tokens) && tokens[idx] != "}" {
			key := tokens[idx]
			if strings.HasPrefix(key, "\"") || strings.HasPrefix(key, "'") {
				parsed, err := parseJSString(key)
				if err != nil {
					return nil, idx, err
				}
				key = parsed
			}
			idx++
			if idx >= len(tokens) || tokens[idx] != ":" {
				return nil, idx, fmt.Errorf("expected colon in object")
			}
			idx++
			val, next, err := parseJSValue(tokens, idx)
			if err != nil {
				return nil, next, err
			}
			obj[key] = val
			idx = next
			if idx < len(tokens) && tokens[idx] == "," {
				idx++
			}
		}
		if idx >= len(tokens) || tokens[idx] != "}" {
			return nil, idx, fmt.Errorf("unterminated object")
		}
		return obj, idx + 1, nil
	}
	if tok == "[" {
		arr := make([]any, 0)
		idx++
		for idx < len(tokens) && tokens[idx] != "]" {
			val, next, err := parseJSValue(tokens, idx)
			if err != nil {
				return nil, next, err
			}
			arr = append(arr, val)
			idx = next
			if idx < len(tokens) && tokens[idx] == "," {
				idx++
			}
		}
		if idx >= len(tokens) || tokens[idx] != "]" {
			return nil, idx, fmt.Errorf("unterminated array")
		}
		return arr, idx + 1, nil
	}
	val, err := parseJSToken(tok)
	return val, idx + 1, err
}

func parseJSToken(tok string) (any, error) {
	tok = strings.TrimSpace(tok)
	switch tok {
	case "null", "undefined":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if strings.HasPrefix(tok, "\"") || strings.HasPrefix(tok, "'") {
		return parseJSString(tok)
	}
	if i, err := strconv.ParseInt(tok, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil {
		return f, nil
	}
	return tok, nil
}

func parseJSString(s string) (string, error) {
	if len(s) < 2 || s[0] != s[len(s)-1] {
		return "", fmt.Errorf("invalid JS string literal")
	}
	body := s[1 : len(s)-1]
	if !strings.Contains(body, "\\") {
		return body, nil
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			b.WriteByte(body[i])
			continue
		}
		i++
		if i >= len(body) {
			break
		}
		switch body[i] {
		case '\'', '"', '\\':
			b.WriteByte(body[i])
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0':
			b.WriteByte(0)
		case 'x':
			if i+2 >= len(body) {
				return "", fmt.Errorf("invalid hex escape")
			}
			v, err := strconv.ParseInt(body[i+1:i+3], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(v))
			i += 2
		case 'u':
			if i+4 >= len(body) {
				return "", fmt.Errorf("invalid unicode escape")
			}
			v, err := strconv.ParseInt(body[i+1:i+5], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(v))
			i += 4
		default:
			b.WriteByte(body[i])
		}
	}
	return b.String(), nil
}

func fanqieCategoryTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var data []map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	tags := make([]string, 0, len(data))
	for _, item := range data {
		if name := stringValue(item["Name"]); name != "" {
			tags = append(tags, name)
		}
	}
	return tags
}

func fanqieSortKey(item map[string]any) int64 {
	if value := int64Value(item["realChapterOrder"]); value != 0 {
		return value
	}
	return int64Value(item["itemId"])
}

func mapValue(value any, keys ...string) map[string]any {
	if len(keys) > 0 {
		current, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for _, key := range keys {
			value = current[key]
			current, ok = value.(map[string]any)
			if !ok {
				return nil
			}
		}
		return current
	}
	current, _ := value.(map[string]any)
	return current
}

func sliceValue(value any) []any {
	slice, _ := value.([]any)
	return slice
}

func stringSliceValue(value any) []string {
	items := sliceValue(value)
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringValue(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	case float64:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(v, 10, 64)
		return i
	default:
		return 0
	}
}
