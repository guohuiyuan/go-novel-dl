package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

const qidianBooklistPageURL = "https://m.qidian.com/booklist/?source=pc_jump"

var qidianDiscoveryLists = []DiscoveryList{
	{ID: "yuepiao", Name: "月票榜", Description: "读者月票支持最多的连载作品", Kind: "ranking", UpdateFrequency: "实时更新"},
	{ID: "hotsales", Name: "畅销榜", Description: "近期付费阅读与销售热度领先的作品", Kind: "ranking", UpdateFrequency: "实时更新"},
	{ID: "readindex", Name: "阅读指数榜", Description: "综合阅读量与追读表现的阅读指数排行", Kind: "ranking", UpdateFrequency: "每日更新"},
	{ID: "newfans", Name: "新粉丝榜", Description: "近期新增粉丝增长最快的作品", Kind: "ranking", UpdateFrequency: "每日更新"},
	{ID: "rec", Name: "推荐榜", Description: "读者推荐票集中的热门作品", Kind: "ranking", UpdateFrequency: "实时更新"},
	{ID: "update", Name: "更新榜", Description: "保持稳定更新的人气作品", Kind: "ranking", UpdateFrequency: "每日更新"},
	{ID: "sign", Name: "签约榜", Description: "起点签约作品中的热度排行", Kind: "ranking", UpdateFrequency: "每周更新"},
	{ID: "newbook", Name: "新书榜", Description: "近期上架新书中的热门作品", Kind: "ranking", UpdateFrequency: "每日更新"},
	{ID: "newauthor", Name: "新人榜", Description: "新锐作者与新人作品排行", Kind: "ranking", UpdateFrequency: "每周更新"},
}

var _ DiscoveryProvider = (*QidianSite)(nil)

func (s *QidianSite) DiscoveryLists(ctx context.Context) ([]DiscoveryList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lists := append([]DiscoveryList(nil), qidianDiscoveryLists...)
	markup, err := s.fetchQidianHTML(
		ctx,
		qidianBooklistPageURL,
		qidianMobileHeaders("https://m.qidian.com/"),
	)
	if err == nil {
		lists = append(lists, parseQidianBooklistCatalog(markup)...)
	}
	return cloneDiscoveryLists(lists), nil
}

func (s *QidianSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	listID = strings.TrimSpace(listID)
	if booklistID, ok := strings.CutPrefix(listID, "booklist:"); ok {
		booklistID = strings.TrimSpace(booklistID)
		if booklistID == "" {
			return nil, fmt.Errorf("qidian booklist id is empty")
		}
		markup, err := s.fetchQidianHTML(
			ctx,
			"https://m.qidian.com/booklist/detail/"+booklistID+"/",
			qidianMobileHeaders("https://m.qidian.com/booklist/"),
		)
		if err != nil {
			return nil, err
		}
		return parseQidianBooklistBooks(markup, normalizeDiscoveryLimit(limit, 20))
	}
	known := false
	for _, item := range qidianDiscoveryLists {
		if item.ID == listID {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("qidian discovery list %q not found", listID)
	}
	limit = normalizeDiscoveryLimit(limit, 20)
	rawURL := fmt.Sprintf("https://m.qidian.com/rank/%s/", listID)
	markup, err := s.fetchQidianHTML(ctx, rawURL, qidianMobileHeaders("https://m.qidian.com/rank/"))
	if err != nil {
		return nil, err
	}
	return parseQidianDiscoveryResults(markup, limit)
}

type qidianBooklistCatalogPayload struct {
	PageContext struct {
		PageProps struct {
			PageData struct {
				RecBookList      []qidianBooklistMetadata `json:"recBookList"`
				FeaturedBookList []qidianBooklistMetadata `json:"featuredBookList"`
				TopCollectList   []qidianBooklistMetadata `json:"topCollectList"`
			} `json:"pageData"`
		} `json:"pageProps"`
	} `json:"pageContext"`
}

type qidianBooklistSample struct {
	BookID string `json:"bookId"`
}

type qidianBooklistMetadata struct {
	ID           int64                  `json:"id"`
	Name         string                 `json:"bookListName"`
	Description  string                 `json:"bookListDesc"`
	BookCount    int                    `json:"bookCount"`
	CollectCount int                    `json:"collectCount"`
	Label        string                 `json:"label"`
	OwnerName    string                 `json:"ownerName"`
	Samples      []qidianBooklistSample `json:"samples"`
}

type qidianBooklistDetailPayload struct {
	PageContext struct {
		PageProps struct {
			PageData struct {
				BookListInfo struct {
					ID        int64                    `json:"id"`
					Name      string                   `json:"bookListName"`
					BookCount int                      `json:"bookCount"`
					Books     []qidianBooklistBookInfo `json:"books"`
				} `json:"bookListInfo"`
			} `json:"pageData"`
		} `json:"pageProps"`
	} `json:"pageContext"`
}

type qidianBooklistBookInfo struct {
	BookID         string `json:"bookId"`
	Title          string `json:"bookName"`
	Author         string `json:"authorName"`
	Description    string `json:"desc"`
	Recommendation string `json:"recommendation"`
	Channel        string `json:"chanName"`
	Status         string `json:"bookStatus"`
	LatestChapter  string `json:"updChapterName"`
}

func parseQidianSSRPageData(markup string, target any) error {
	doc, err := parseHTML(markup)
	if err != nil {
		return err
	}
	script := findFirst(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode &&
			node.Data == "script" &&
			attrValue(node, "id") == "vite-plugin-ssr_pageContext"
	})
	if script == nil {
		return fmt.Errorf("qidian SSR page data not found")
	}
	payload := strings.TrimSpace(nodeText(script))
	if payload == "" {
		return fmt.Errorf("qidian SSR page data is empty")
	}
	if err := json.Unmarshal([]byte(payload), target); err != nil {
		return fmt.Errorf("decode qidian SSR page data: %w", err)
	}
	return nil
}

func parseQidianBooklistCatalog(markup string) []DiscoveryList {
	var payload qidianBooklistCatalogPayload
	if err := parseQidianSSRPageData(markup, &payload); err != nil {
		return nil
	}
	pageData := payload.PageContext.PageProps.PageData
	items := make([]qidianBooklistMetadata, 0, len(pageData.RecBookList)+len(pageData.FeaturedBookList)+len(pageData.TopCollectList))
	items = append(items, pageData.FeaturedBookList...)
	items = append(items, pageData.TopCollectList...)
	items = append(items, pageData.RecBookList...)

	lists := make([]DiscoveryList, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if item.ID <= 0 {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		name := cleanText(item.Name)
		if name == "" {
			continue
		}
		description := cleanText(item.Description)
		details := make([]string, 0, 2)
		if owner := strings.TrimSpace(item.OwnerName); owner != "" {
			details = append(details, "书单主："+owner)
		}
		if item.CollectCount > 0 {
			details = append(details, fmt.Sprintf("收藏 %d", item.CollectCount))
		}
		if len(details) > 0 {
			if description == "" {
				description = strings.Join(details, " · ")
			} else {
				description += " · " + strings.Join(details, " · ")
			}
		}
		lists = append(lists, DiscoveryList{
			ID:              fmt.Sprintf("booklist:%d", item.ID),
			Name:            name,
			Description:     description,
			Kind:            "booklist",
			UpdateFrequency: "起点书单",
			BookCount:       item.BookCount,
			CoverURL:        qidianBooklistCoverURL(item.Samples),
		})
	}
	return lists
}

func qidianBooklistCoverURL(samples []qidianBooklistSample) string {
	for _, sample := range samples {
		if bookID := strings.TrimSpace(sample.BookID); bookID != "" {
			return qidianDiscoveryCoverURL(bookID)
		}
	}
	return ""
}

func parseQidianBooklistBooks(markup string, limit int) ([]model.SearchResult, error) {
	var payload qidianBooklistDetailPayload
	if err := parseQidianSSRPageData(markup, &payload); err != nil {
		return nil, err
	}
	books := payload.PageContext.PageProps.PageData.BookListInfo.Books
	if len(books) == 0 {
		return nil, fmt.Errorf("qidian booklist has no books")
	}
	results := make([]model.SearchResult, 0, len(books))
	seen := make(map[string]struct{}, len(books))
	for _, book := range books {
		bookID := strings.TrimSpace(book.BookID)
		title := cleanText(book.Title)
		if bookID == "" || title == "" {
			continue
		}
		if _, exists := seen[bookID]; exists {
			continue
		}
		seen[bookID] = struct{}{}
		description := cleanText(book.Description)
		if description == "" {
			description = cleanText(book.Recommendation)
		}
		results = append(results, model.SearchResult{
			Site:          "qidian",
			BookID:        bookID,
			Title:         title,
			Author:        cleanText(book.Author),
			Description:   description,
			URL:           fmt.Sprintf("https://www.qidian.com/book/%s/", bookID),
			LatestChapter: cleanText(book.LatestChapter),
			CoverURL:      qidianDiscoveryCoverURL(bookID),
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func qidianDiscoveryCoverURL(bookID string) string {
	bookID = strings.TrimSpace(bookID)
	if bookID == "" {
		return ""
	}
	return "https://bookcover.yuewen.com/qdbimg/349573/" + bookID + "/180"
}

func parseQidianDiscoveryResults(markup string, limit int) ([]model.SearchResult, error) {
	doc, err := parseHTML(markup)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}

	results := make([]model.SearchResult, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, link := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && qidianSearchBookID(attrValue(n, "href")) != ""
	}) {
		bookID := qidianSearchBookID(attrValue(link, "href"))
		if bookID == "" {
			continue
		}
		if _, ok := seen[bookID]; ok {
			continue
		}
		seen[bookID] = struct{}{}

		card := qidianDiscoveryCard(link)
		title := qidianFirstText(card, qidianByTag("h2"), qidianByTag("h3"))
		if title == "" {
			title = strings.TrimSpace(strings.TrimSuffix(attrValue(link, "title"), "最新章节在线阅读"))
		}
		if title == "" {
			continue
		}

		subtitle := qidianFirstText(card, qidianByClassPart("subTitle"))
		author, _ := qidianDiscoverySubtitle(subtitle)
		description := qidianFirstText(card,
			qidianByClassPart("bookDesc"),
			qidianByClassPart("desc"),
		)
		cover := attrValue(findFirst(card, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "img"
		}), "data-src")
		if cover == "" {
			cover = attrValue(findFirst(card, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "img"
			}), "src")
		}

		results = append(results, model.SearchResult{
			Site:        "qidian",
			BookID:      bookID,
			Title:       title,
			Author:      author,
			Description: description,
			URL:         fmt.Sprintf("https://www.qidian.com/book/%s/", bookID),
			CoverURL:    absolutizeURL("https://m.qidian.com", cover),
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func qidianDiscoveryCard(link *html.Node) *html.Node {
	for current := link.Parent; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && hasClass(current, "y-list__item") {
			return current
		}
	}
	return link
}

func qidianDiscoverySubtitle(value string) (string, string) {
	value = cleanText(value)
	if value == "" {
		return "", ""
	}
	parts := make([]string, 0, 3)
	for _, part := range strings.Split(value, "·") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	case 2:
		return parts[0], parts[1]
	default:
		return parts[0], strings.Join(parts[1:], " · ")
	}
}
