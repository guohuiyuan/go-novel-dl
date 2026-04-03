package exporter

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	stdhtml "html"
	"html/template"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	xdraw "golang.org/x/image/draw"
	xwebp "golang.org/x/image/webp"
)

type Service struct{}

type epubAsset struct {
	ID        string
	Href      string
	MediaType string
	Data      []byte
}

type epubPackage struct {
	OPF          string
	Nav          string
	ChapterFiles map[string]string
	Assets       []*epubAsset
}

type chapterBlock struct {
	Paragraph string
	ImageURL  string
}

type epubAssetFetcher struct {
	client       *http.Client
	assets       []*epubAsset
	byURL        map[string]*epubAsset
	inflight     map[string]*assetFetchFuture
	counter      int
	referer      string
	aggressiveES bool
	mu           sync.Mutex
}

type assetFetchFuture struct {
	done  chan struct{}
	asset *epubAsset
	err   error
}

const defaultAssetUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

const (
	chapterImagePlaceholder             = "[\u56fe\u7247]"
	chapterImageLabelSimplified         = "\u56fe\u7247"
	chapterImageLabelTraditional        = "\u5716\u7247"
	chapterIllustrationLabelSimplified  = "\u63d2\u56fe"
	chapterIllustrationLabelTraditional = "\u63d2\u5716"
)

var chapterImageRe = regexp.MustCompile(fmt.Sprintf("^\\[(?:%s|%s|%s|%s|\\?\\?)\\]\\s*(\\S+)\\s*$",
	chapterImageLabelSimplified,
	chapterImageLabelTraditional,
	chapterIllustrationLabelSimplified,
	chapterIllustrationLabelTraditional,
))

var markdownImageRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)
var htmlImgTagRe = regexp.MustCompile(`(?is)<img\b[^>]*>`)
var htmlAttrDoubleQuotedRe = regexp.MustCompile(`(?i)([a-z0-9_:-]+)\s*=\s*"([^"]*)"`)
var htmlAttrSingleQuotedRe = regexp.MustCompile(`(?i)([a-z0-9_:-]+)\s*=\s*'([^']*)'`)

func New() *Service {
	return &Service{}
}

func (s *Service) Export(book *model.Book, site string, cfg config.OutputConfig, outputDir string, formats []string) ([]string, error) {
	if book == nil {
		return nil, fmt.Errorf("book is nil")
	}
	if len(formats) == 0 {
		formats = cfg.Formats
	}
	if len(formats) == 0 {
		formats = []string{"txt"}
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}

	created := make([]string, 0, len(formats))
	for _, format := range formats {
		format = strings.ToLower(strings.TrimSpace(format))
		if format == "" {
			continue
		}

		filename := buildFilename(book, cfg, format)
		path := filepath.Join(outputDir, sanitize(site), filename)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return created, err
		}

		switch format {
		case "txt":
			if err := os.WriteFile(path, renderTXT(book), 0o644); err != nil {
				return created, err
			}
		case "html":
			if err := os.WriteFile(path, renderHTML(book), 0o644); err != nil {
				return created, err
			}
		case "epub":
			if err := renderEPUB(path, book); err != nil {
				return created, err
			}
		default:
			return created, fmt.Errorf("unsupported export format: %s", format)
		}

		created = append(created, path)
	}

	return created, nil
}

func buildFilename(book *model.Book, cfg config.OutputConfig, format string) string {
	name := cfg.FilenameTemplate
	if strings.TrimSpace(name) == "" {
		name = "{title}_{author}"
	}
	name = strings.ReplaceAll(name, "{title}", fallback(book.Title, book.ID))
	name = strings.ReplaceAll(name, "{author}", fallback(book.Author, "unknown"))
	name = sanitize(name)
	if cfg.AppendTimestamp {
		name += "_" + time.Now().Format("20060102_150405")
	}
	return name + "." + extensionFor(format)
}

func extensionFor(format string) string {
	switch format {
	case "epub":
		return "epub"
	case "html":
		return "html"
	default:
		return "txt"
	}
}

func renderTXT(book *model.Book) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s\n", book.Title)
	fmt.Fprintf(&buf, "Author: %s\n", book.Author)
	if book.Description != "" {
		fmt.Fprintf(&buf, "\n%s\n", book.Description)
	}

	for _, chapter := range book.Chapters {
		fmt.Fprintf(&buf, "\n\n# %s\n\n%s\n", chapter.Title, normalizeTXTChapterContent(chapter.Content))
	}

	return buf.Bytes()
}

func normalizeTXTChapterContent(content string) string {
	blocks := parseChapterBlocks(content)
	if len(blocks) == 0 {
		return strings.TrimSpace(normalizeExportInlineWhitespace(strings.ReplaceAll(content, "\r\n", "\n")))
	}
	parts := make([]string, 0, len(blocks))
	hasTextParagraph := false
	for _, block := range blocks {
		if imageURL := strings.TrimSpace(block.ImageURL); imageURL != "" {
			parts = append(parts, chapterImagePlaceholder+" "+imageURL)
			continue
		}
		paragraph := strings.TrimSpace(normalizeExportInlineWhitespace(strings.ReplaceAll(block.Paragraph, "\r\n", "\n")))
		if paragraph == "" {
			continue
		}
		lines := strings.Split(paragraph, "\n")
		cleanedLines := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			cleanedLines = append(cleanedLines, line)
		}
		if len(cleanedLines) == 0 {
			continue
		}
		for idx, line := range cleanedLines {
			if hasTextParagraph || idx > 0 {
				line = "    " + line
			}
			parts = append(parts, line)
			hasTextParagraph = true
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(normalizeExportInlineWhitespace(strings.ReplaceAll(content, "\r\n", "\n")))
	}
	return strings.Join(parts, "\n\n\n")
}

func renderHTML(book *model.Book) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title>", escapeHTML(book.Title))
	buf.WriteString("<style>body{font-family:Georgia,serif;max-width:900px;margin:40px auto;padding:0 16px;line-height:1.8;}h1,h2{line-height:1.2;}article{margin:24px 0 40px;}pre{white-space:pre-wrap;font-family:inherit;}</style>")
	buf.WriteString("</head><body>")
	fmt.Fprintf(&buf, "<h1>%s</h1><p><strong>%s</strong></p>", escapeHTML(book.Title), escapeHTML(book.Author))
	if book.Description != "" {
		fmt.Fprintf(&buf, "<p>%s</p>", escapeHTML(book.Description))
	}
	for _, chapter := range book.Chapters {
		fmt.Fprintf(&buf, "<article><h2>%s</h2><pre>%s</pre></article>", escapeHTML(chapter.Title), escapeHTML(chapter.Content))
	}
	buf.WriteString("</body></html>")
	return buf.Bytes()
}

func renderEPUB(path string, book *model.Book) error {
	if strings.EqualFold(strings.TrimSpace(book.Site), "esjzone") {
		return renderEPUBLikeESJScript(path, book)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	if err := writeStoredFile(zw, "mimetype", []byte("application/epub+zip")); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "META-INF/container.xml", []byte(containerXML)); err != nil {
		_ = zw.Close()
		return err
	}

	pkg, err := buildEPUBContent(book)
	if err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "OEBPS/content.opf", []byte(pkg.OPF)); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "OEBPS/nav.xhtml", []byte(pkg.Nav)); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "OEBPS/styles.css", []byte(defaultEPUBCSS)); err != nil {
		_ = zw.Close()
		return err
	}
	for _, asset := range pkg.Assets {
		if err := writeZipFile(zw, "OEBPS/"+asset.Href, asset.Data); err != nil {
			_ = zw.Close()
			return err
		}
	}
	chapterNames := make([]string, 0, len(pkg.ChapterFiles))
	for name := range pkg.ChapterFiles {
		chapterNames = append(chapterNames, name)
	}
	sort.Strings(chapterNames)
	for _, name := range chapterNames {
		body := pkg.ChapterFiles[name]
		if err := writeZipFile(zw, "OEBPS/"+name, []byte(body)); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func renderEPUBLikeESJScript(path string, book *model.Book) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	if err := writeStoredFile(zw, "mimetype", []byte("application/epub+zip")); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipDir(zw, "META-INF/"); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "META-INF/container.xml", []byte(esjContainerXML)); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipDir(zw, "OEBPS/"); err != nil {
		_ = zw.Close()
		return err
	}

	pkg, err := buildEPUBContentLikeESJScript(book)
	if err != nil {
		_ = zw.Close()
		return err
	}
	assetByHref := make(map[string]*epubAsset, len(pkg.Assets))
	for _, asset := range pkg.Assets {
		assetByHref[asset.Href] = asset
	}
	writtenAsset := make(map[string]struct{}, len(pkg.Assets))
	if cover, ok := assetByHref["cover.png"]; ok {
		if err := writeZipFile(zw, "OEBPS/"+cover.Href, cover.Data); err != nil {
			_ = zw.Close()
			return err
		}
		writtenAsset[cover.Href] = struct{}{}
	} else if cover, ok := assetByHref["cover.jpg"]; ok {
		if err := writeZipFile(zw, "OEBPS/"+cover.Href, cover.Data); err != nil {
			_ = zw.Close()
			return err
		}
		writtenAsset[cover.Href] = struct{}{}
	}
	chapterNames := make([]string, 0, len(pkg.ChapterFiles))
	for name := range pkg.ChapterFiles {
		chapterNames = append(chapterNames, name)
	}
	sort.Slice(chapterNames, func(i, j int) bool {
		in := 0
		jn := 0
		_, _ = fmt.Sscanf(chapterNames[i], "chap_%d.xhtml", &in)
		_, _ = fmt.Sscanf(chapterNames[j], "chap_%d.xhtml", &jn)
		return in < jn
	})
	for _, name := range chapterNames {
		chapterNo := 0
		_, _ = fmt.Sscanf(name, "chap_%d.xhtml", &chapterNo)
		imagePrefix := fmt.Sprintf("img_%d_", chapterNo-1)
		chapterImages := make([]string, 0)
		for href := range assetByHref {
			if strings.HasPrefix(href, imagePrefix) {
				chapterImages = append(chapterImages, href)
			}
		}
		sort.Strings(chapterImages)
		for _, href := range chapterImages {
			if _, ok := writtenAsset[href]; ok {
				continue
			}
			if err := writeZipFile(zw, "OEBPS/"+href, assetByHref[href].Data); err != nil {
				_ = zw.Close()
				return err
			}
			writtenAsset[href] = struct{}{}
		}

		body := pkg.ChapterFiles[name]
		if err := writeZipFile(zw, "OEBPS/"+name, []byte(body)); err != nil {
			_ = zw.Close()
			return err
		}
	}
	remaining := make([]string, 0)
	for href := range assetByHref {
		if _, ok := writtenAsset[href]; ok {
			continue
		}
		remaining = append(remaining, href)
	}
	sort.Strings(remaining)
	for _, href := range remaining {
		if err := writeZipFile(zw, "OEBPS/"+href, assetByHref[href].Data); err != nil {
			_ = zw.Close()
			return err
		}
	}
	if err := writeZipFile(zw, "OEBPS/nav.xhtml", []byte(pkg.Nav)); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "OEBPS/content.opf", []byte(pkg.OPF)); err != nil {
		_ = zw.Close()
		return err
	}
	return zw.Close()
}

func buildEPUBContentLikeESJScript(book *model.Book) (*epubPackage, error) {
	type chapterImage struct {
		url      string
		fileName string
	}
	type chapterBuild struct {
		title  string
		name   string
		body   string
		images []chapterImage
	}

	chapterBuilds := make([]chapterBuild, 0, len(book.Chapters))
	allImageURLs := make([]string, 0)
	for idx, chapter := range book.Chapters {
		fileName := fmt.Sprintf("chap_%d.xhtml", idx+1)
		title := fallback(strings.TrimSpace(chapter.Title), fmt.Sprintf("第%d章", idx+1))
		blocks := parseChapterBlocks(chapter.Content)
		imageIndex := 0
		images := make([]chapterImage, 0)
		bodyParts := make([]string, 0, len(blocks))
		hasTextParagraph := false
		hasImageBlock := false
		for _, block := range blocks {
			if strings.TrimSpace(block.ImageURL) != "" {
				imgName := fmt.Sprintf("img_%d_%d", idx, imageIndex)
				images = append(images, chapterImage{url: strings.TrimSpace(block.ImageURL), fileName: imgName})
				allImageURLs = append(allImageURLs, strings.TrimSpace(block.ImageURL))
				bodyParts = append(bodyParts, fmt.Sprintf(`<p><img class="fr-fic fr-dib" src="%s" style="max-width: 100%%;" /></p>`, imgName))
				hasImageBlock = true
				imageIndex++
				continue
			}
			if paragraphHTML := buildEPUBParagraphHTML(block.Paragraph, !hasTextParagraph); paragraphHTML != "" {
				bodyParts = append(bodyParts, paragraphHTML)
				hasTextParagraph = true
			}
		}
		if hasImageBlock && !hasTextParagraph {
			bodyParts = append(bodyParts, "<p><br /></p>                                  ")
		}
		chapterXHTML := fmt.Sprintf(
			"<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"+
				"            <html xmlns=\"http://www.w3.org/1999/xhtml\">\n"+
				"              <head><title>%s</title><style>%s</style></head>\n"+
				"              <body>\n"+
				"                <h2>%s</h2>\n"+
				"                <div>%s</div>\n"+
				"              </body>\n"+
				"            </html>",
			escapeHTML(title),
			esjEPUBParagraphCSS,
			escapeHTML(title),
			strings.Join(bodyParts, "\n"),
		)
		chapterBuilds = append(chapterBuilds, chapterBuild{title: title, name: fileName, body: chapterXHTML, images: images})
	}

	imageBlobCache := make(map[string]struct {
		data      []byte
		mediaType string
		err       error
	})
	var cacheMu sync.Mutex
	fetchOne := func(rawURL string) {
		cacheMu.Lock()
		if _, ok := imageBlobCache[rawURL]; ok {
			cacheMu.Unlock()
			return
		}
		cacheMu.Unlock()

		data, mediaType, err := downloadAssetForESJ(rawURL, "https://www.esjzone.cc/")
		if err == nil {
			data, mediaType, err = compressImageForESJ(data, mediaType)
		}
		cacheMu.Lock()
		imageBlobCache[rawURL] = struct {
			data      []byte
			mediaType string
			err       error
		}{data: data, mediaType: mediaType, err: err}
		cacheMu.Unlock()
	}
	prefetchList := uniqueStrings(allImageURLs)
	jobs := make(chan string, len(prefetchList))
	var wg sync.WaitGroup
	workerCount := 6
	if workerCount > len(prefetchList) {
		workerCount = len(prefetchList)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				fetchOne(item)
			}
		}()
	}
	for _, item := range prefetchList {
		jobs <- item
	}
	close(jobs)
	wg.Wait()

	assets := make([]*epubAsset, 0)
	manifestItems := make([]string, 0)
	spineItems := make([]string, 0, len(chapterBuilds))
	navPoints := make([]string, 0, len(chapterBuilds))
	chapterFiles := make(map[string]string, len(chapterBuilds))

	if strings.TrimSpace(book.CoverURL) != "" {
		coverData, coverType, err := downloadAssetForESJ(book.CoverURL, "https://www.esjzone.cc/")
		if err == nil && len(coverData) > 0 {
			ext := imageExtensionForESJ(coverType)
			coverName := "cover." + ext
			assets = append(assets, &epubAsset{ID: "cover-image", Href: coverName, MediaType: coverType, Data: coverData})
			manifestItems = append(manifestItems, fmt.Sprintf(`<item id="cover-image" href="%s" media-type="%s" properties="cover-image"/>`, coverName, coverType))
		}
	}

	for idx, chap := range chapterBuilds {
		chapterID := fmt.Sprintf("chap_%d", idx+1)
		chapterFiles[chap.name] = chap.body
		spineItems = append(spineItems, fmt.Sprintf(`<itemref idref="%s"/>`, chapterID))
		navPoints = append(navPoints, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, chap.name, escapeHTML(chap.title)))

		for _, image := range chap.images {
			cacheMu.Lock()
			cached := imageBlobCache[image.url]
			cacheMu.Unlock()
			if cached.err != nil || len(cached.data) == 0 {
				continue
			}
			ext := imageExtensionForESJ(cached.mediaType)
			name := image.fileName + "." + ext
			chapterFiles[chap.name] = strings.ReplaceAll(chapterFiles[chap.name], `src="`+image.fileName+`"`, `src="`+name+`"`)
			assetID := strings.ReplaceAll(name, ".", "_")
			assets = append(assets, &epubAsset{ID: assetID, Href: name, MediaType: cached.mediaType, Data: cached.data})
			manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="%s" />`, assetID, name, cached.mediaType))
		}
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="application/xhtml+xml"/>`, chapterID, chap.name))
	}
	manifestItems = append(manifestItems, `<item id="nav" href="nav.xhtml" properties="nav" media-type="application/xhtml+xml"/>`)

	nav := fmt.Sprintf(esjNavTemplate, strings.Join(navPoints, ""))
	coverMeta := ""
	for _, item := range manifestItems {
		if strings.Contains(item, `id="cover-image"`) {
			coverMeta = `<meta name="cover" content="cover-image" />`
			break
		}
	}
	uid := fmt.Sprintf("id-%d", time.Now().UTC().UnixMilli())
	opf := fmt.Sprintf(esjContentOPFTemplate,
		escapeHTML(fallback(book.Title, "未知书名")),
		uid,
		escapeHTML(fallback(book.Author, "")),
		time.Now().UTC().Format(time.RFC3339),
		coverMeta,
		strings.Join(manifestItems, "\n"),
		strings.Join(spineItems, "\n"),
	)

	return &epubPackage{OPF: opf, Nav: nav, ChapterFiles: chapterFiles, Assets: assets}, nil
}

func downloadAssetForESJ(rawURL, referer string) ([]byte, string, error) {
	rawURL = normalizeAssetURL(rawURL, referer)
	if rawURL == "" {
		return nil, "", fmt.Errorf("empty asset url")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", defaultAssetUserAgent)
		req.Header.Set("Accept", "image/*,*/*;q=0.8")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(250*(attempt+1)) * time.Millisecond)
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			time.Sleep(time.Duration(250*(attempt+1)) * time.Millisecond)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
			time.Sleep(time.Duration(250*(attempt+1)) * time.Millisecond)
			continue
		}
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
		if mediaType == "" {
			if ext := strings.ToLower(path.Ext(resp.Request.URL.Path)); ext != "" {
				mediaType = mime.TypeByExtension(ext)
			}
		}
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		return data, normalizeImageMediaTypeForESJ(mediaType), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("download failed: %s", rawURL)
	}
	return nil, "", lastErr
}

func compressImageForESJ(data []byte, mediaType string) ([]byte, string, error) {
	img, err := decodeRasterImage(data, normalizeImageMediaTypeForESJ(mediaType))
	if err != nil {
		return data, normalizeImageMediaTypeForESJ(mediaType), nil
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return data, normalizeImageMediaTypeForESJ(mediaType), nil
	}
	maxWidth := 800
	targetW := width
	targetH := height
	if width > maxWidth {
		targetW = maxWidth
		targetH = int(math.Round(float64(height) * (float64(maxWidth) / float64(width))))
		if targetH < 1 {
			targetH = 1
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			canvas.Set(x, y, image.White)
		}
	}
	xdraw.ApproxBiLinear.Scale(canvas, canvas.Bounds(), img, bounds, xdraw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 70}); err != nil {
		return data, normalizeImageMediaTypeForESJ(mediaType), nil
	}
	return out.Bytes(), "image/jpeg", nil
}

func normalizeImageMediaTypeForESJ(mediaType string) string {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch mediaType {
	case "image/png", "image/gif", "image/webp", "image/bmp", "image/jpeg", "image/jpg":
		if mediaType == "image/jpg" {
			return "image/jpeg"
		}
		return mediaType
	default:
		return "image/jpeg"
	}
}

func imageExtensionForESJ(mediaType string) string {
	switch normalizeImageMediaTypeForESJ(mediaType) {
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/bmp":
		return "bmp"
	default:
		return "jpg"
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func buildEPUBContent(book *model.Book) (*epubPackage, error) {
	fetcher := newEPUBAssetFetcher(book)
	manifestItems := []string{
		`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`,
		`<item id="css" href="styles.css" media-type="text/css"/>`,
	}
	spineItems := make([]string, 0, len(book.Chapters))
	navPoints := make([]string, 0, len(book.Chapters))
	chapterFiles := make(map[string]string, len(book.Chapters))
	chapterBlocksByFile := make(map[string][]chapterBlock, len(book.Chapters))
	chapterByFile := make(map[string]model.Chapter, len(book.Chapters))
	imageURLs := make([]string, 0)
	seenImageURL := make(map[string]struct{})

	coverImageHref := ""
	coverImageID := ""
	if asset, err := fetcher.ResolveImage(book.CoverURL); err == nil && asset != nil {
		coverImageHref = asset.Href
		coverImageID = asset.ID
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="%s" properties="cover-image"/>`, asset.ID, asset.Href, asset.MediaType))
	}

	for idx, chapter := range book.Chapters {
		fileName := fmt.Sprintf("chapter-%03d.xhtml", idx+1)
		itemID := fmt.Sprintf("chapter-%03d", idx+1)
		blocks := parseChapterBlocks(chapter.Content)
		chapterBlocksByFile[fileName] = blocks
		chapterByFile[fileName] = chapter
		for _, block := range blocks {
			if block.ImageURL == "" {
				continue
			}
			if _, ok := seenImageURL[block.ImageURL]; ok {
				continue
			}
			seenImageURL[block.ImageURL] = struct{}{}
			imageURLs = append(imageURLs, block.ImageURL)
		}
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="application/xhtml+xml"/>`, itemID, fileName))
		spineItems = append(spineItems, fmt.Sprintf(`<itemref idref="%s"/>`, itemID))
		navPoints = append(navPoints, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, fileName, escapeHTML(chapter.Title)))
	}

	fetcher.PrefetchImages(imageURLs, 6)
	for fileName, chapter := range chapterByFile {
		chapterFiles[fileName] = buildChapterPageWithBlocks(book.Title, chapter, chapterBlocksByFile[fileName], fetcher)
	}

	for _, asset := range fetcher.Assets() {
		if asset.Href == coverImageHref {
			continue
		}
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="%s"/>`, asset.ID, asset.Href, asset.MediaType))
	}

	coverMeta := ""
	if coverImageID != "" {
		coverMeta = fmt.Sprintf(`<meta name="cover" content="%s"/>`, coverImageID)
	}

	bookUUID := makeBookUUID(book)
	opf := fmt.Sprintf(contentOPFTemplate,
		bookUUID,
		escapeHTML(fallback(book.Title, book.ID)),
		escapeHTML(fallback(book.Author, "unknown")),
		time.Now().UTC().Format(time.RFC3339),
		coverMeta,
		strings.Join(manifestItems, "\n    "),
		strings.Join(spineItems, "\n    "),
	)
	navTitle := escapeHTML(fallback(book.Title, book.ID))
	nav := fmt.Sprintf(navTemplate, navTitle, navTitle, strings.Join(navPoints, "\n      "))
	return &epubPackage{
		OPF:          opf,
		Nav:          nav,
		ChapterFiles: chapterFiles,
		Assets:       fetcher.Assets(),
	}, nil
}

func makeBookUUID(book *model.Book) string {
	seed := fmt.Sprintf("%s:%s:%s", book.Site, book.ID, book.Title)
	sum := sha1.Sum([]byte(seed))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}

func buildCoverPage(title, author, description, coverImageHref string) string {
	image := ""
	if strings.TrimSpace(coverImageHref) != "" {
		image = fmt.Sprintf(`<figure class="cover-art"><img src="%s" alt="%s"/></figure>`, escapeHTML(coverImageHref), escapeHTML(title))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>%s</title><link rel="stylesheet" type="text/css" href="styles.css"/></head>
<body><section class="cover">%s<h1>%s</h1><p class="author">%s</p><p>%s</p></section></body>
</html>`, escapeHTML(title), image, escapeHTML(title), escapeHTML(author), escapeHTML(description))
}

func buildChapterPageWithBlocks(bookTitle string, chapter model.Chapter, blocks []chapterBlock, fetcher *epubAssetFetcher) string {
	body := make([]string, 0, len(blocks))
	hasTextParagraph := false
	for _, block := range blocks {
		if block.ImageURL == "" {
			if paragraphHTML := buildEPUBParagraphHTML(block.Paragraph, !hasTextParagraph); paragraphHTML != "" {
				body = append(body, paragraphHTML)
				hasTextParagraph = true
			}
			continue
		}
		asset, err := fetcher.ResolveImage(block.ImageURL)
		if err != nil || asset == nil {
			body = append(body, "<p>"+escapeHTML(chapterImagePlaceholder+" "+block.ImageURL)+"</p>")
			continue
		}
		body = append(body, fmt.Sprintf(`<figure class="illustration"><img src="%s" alt="%s"/></figure>`, escapeHTML(asset.Href), escapeHTML(chapter.Title)))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>%s - %s</title><link rel="stylesheet" type="text/css" href="styles.css"/></head>
<body><article><h2>%s</h2>%s</article></body>
</html>`, escapeHTML(bookTitle), escapeHTML(chapter.Title), escapeHTML(chapter.Title), strings.Join(body, ""))
}

func buildEPUBParagraphHTML(paragraph string, firstParagraph bool) string {
	paragraph = strings.TrimSpace(normalizeExportInlineWhitespace(strings.ReplaceAll(paragraph, "\r\n", "\n")))
	if paragraph == "" {
		return ""
	}
	lines := strings.Split(paragraph, "\n")
	escaped := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		escaped = append(escaped, escapeHTML(line))
	}
	if len(escaped) == 0 {
		return ""
	}
	parts := make([]string, 0, len(escaped))
	for idx, line := range escaped {
		className := "novel-paragraph"
		if firstParagraph && idx == 0 {
			className += " novel-paragraph-first"
		}
		parts = append(parts, `<p class="`+className+`">`+line+`</p>`)
	}
	return strings.Join(parts, "")
}

func normalizeExportInlineWhitespace(value string) string {
	replacer := strings.NewReplacer(
		"&nbsp;", " ",
		"&#160;", " ",
		"&#xa0;", " ",
		"\u00a0", " ",
	)
	value = replacer.Replace(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\u202f", " ")
	return value
}

func parseChapterBlocks(content string) []chapterBlock {
	parts := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n\n")
	blocks := make([]chapterBlock, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		paragraphLines := make([]string, 0)
		for _, line := range strings.Split(part, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if imageURL := parseImagePlaceholder(line); imageURL != "" {
				blocks = append(blocks, chapterBlock{ImageURL: imageURL})
				continue
			}
			inlineURLs := collectInlineImageURLs(line)
			for _, imageURL := range inlineURLs {
				blocks = append(blocks, chapterBlock{ImageURL: imageURL})
			}
			cleanedLine := strings.TrimSpace(stripInlineImageMarkup(line))
			if cleanedLine != "" {
				paragraphLines = append(paragraphLines, cleanedLine)
			}
		}
		if len(paragraphLines) > 0 {
			blocks = append(blocks, chapterBlock{Paragraph: strings.Join(paragraphLines, "\n")})
		}
	}
	return blocks
}

func collectInlineImageURLs(text string) []string {
	urls := make([]string, 0)
	seen := make(map[string]struct{})
	for _, match := range markdownImageRe.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			appendInlineImageURL(&urls, seen, match[1])
		}
	}
	for _, tag := range htmlImgTagRe.FindAllString(text, -1) {
		attrs := parseHTMLTagAttrs(tag)
		appendInlineImageURL(&urls, seen, firstNonEmptyValue(attrs, "data-original", "data-src", "data-lazy-src", "data-echo", "src"))
		if primary := firstNonEmptyValue(attrs, "srcset", "data-srcset"); primary != "" {
			appendInlineImageURL(&urls, seen, firstSrcsetURL(primary))
		}
	}
	return urls
}

func parseHTMLTagAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, item := range htmlAttrDoubleQuotedRe.FindAllStringSubmatch(tag, -1) {
		if len(item) != 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(item[1]))
		if key == "" {
			continue
		}
		attrs[key] = item[2]
	}
	for _, item := range htmlAttrSingleQuotedRe.FindAllStringSubmatch(tag, -1) {
		if len(item) != 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(item[1]))
		if key == "" {
			continue
		}
		attrs[key] = item[2]
	}
	return attrs
}

func firstNonEmptyValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[strings.ToLower(key)]); value != "" {
			return value
		}
	}
	return ""
}

func appendInlineImageURL(urls *[]string, seen map[string]struct{}, raw string) {
	normalized := normalizeInlineImageURL(raw)
	if normalized == "" {
		return
	}
	if _, ok := seen[normalized]; ok {
		return
	}
	seen[normalized] = struct{}{}
	*urls = append(*urls, normalized)
}

func normalizeInlineImageURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = stdhtml.UnescapeString(value)
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "javascript:") {
		return ""
	}
	return value
}

func firstSrcsetURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	first := strings.SplitN(value, ",", 2)[0]
	first = strings.TrimSpace(first)
	if first == "" {
		return ""
	}
	parts := strings.Fields(first)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func stripInlineImageMarkup(text string) string {
	text = markdownImageRe.ReplaceAllString(text, "")
	text = htmlImgTagRe.ReplaceAllString(text, "")
	return text
}

func parseImagePlaceholder(value string) string {
	match := chapterImageRe.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func newEPUBAssetFetcher(book *model.Book) *epubAssetFetcher {
	referer := ""
	aggressiveES := false
	if book != nil {
		switch strings.ToLower(strings.TrimSpace(book.Site)) {
		case "linovelib":
			referer = "https://www.linovelib.com/"
		case "esjzone":
			referer = "https://www.esjzone.cc/"
			aggressiveES = true
		}
	}
	return &epubAssetFetcher{
		client:       &http.Client{Timeout: 45 * time.Second},
		byURL:        make(map[string]*epubAsset),
		inflight:     make(map[string]*assetFetchFuture),
		referer:      referer,
		aggressiveES: aggressiveES,
	}
}

func (f *epubAssetFetcher) Assets() []*epubAsset {
	return f.assets
}

func (f *epubAssetFetcher) ResolveImage(rawURL string) (*epubAsset, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil
	}

	f.mu.Lock()
	if asset, ok := f.byURL[rawURL]; ok {
		f.mu.Unlock()
		return asset, nil
	}
	if pending, ok := f.inflight[rawURL]; ok {
		f.mu.Unlock()
		<-pending.done
		return pending.asset, pending.err
	}
	future := &assetFetchFuture{done: make(chan struct{})}
	f.inflight[rawURL] = future
	f.mu.Unlock()

	data, mediaType, finalURL, err := downloadAsset(f.client, rawURL, f.referer)
	if err != nil {
		f.finishInflight(rawURL, nil, err)
		return nil, err
	}
	data, mediaType, err = transcodeRasterToJPEG(data, mediaType)
	if err != nil {
		f.finishInflight(rawURL, nil, err)
		return nil, err
	}
	data, mediaType, err = optimizeImageForEPUB(data, mediaType, f.aggressiveES)
	if err != nil {
		f.finishInflight(rawURL, nil, err)
		return nil, err
	}
	if mediaType != "image/jpeg" {
		err = fmt.Errorf("unsupported epub image media type: %s", mediaType)
		f.finishInflight(rawURL, nil, err)
		return nil, err
	}

	f.mu.Lock()
	f.counter++
	ext := assetExtension(mediaType, finalURL)
	asset := &epubAsset{
		ID:        fmt.Sprintf("image-%03d", f.counter),
		Href:      fmt.Sprintf("images/image-%03d%s", f.counter, ext),
		MediaType: assetMediaType(mediaType, ext),
		Data:      data,
	}
	f.byURL[rawURL] = asset
	f.assets = append(f.assets, asset)
	f.mu.Unlock()
	f.finishInflight(rawURL, asset, nil)
	return asset, nil
}

func (f *epubAssetFetcher) finishInflight(rawURL string, asset *epubAsset, err error) {
	f.mu.Lock()
	pending, ok := f.inflight[rawURL]
	if ok {
		pending.asset = asset
		pending.err = err
		delete(f.inflight, rawURL)
	}
	f.mu.Unlock()
	if ok {
		close(pending.done)
	}
}

func (f *epubAssetFetcher) PrefetchImages(urls []string, workers int) {
	if len(urls) == 0 {
		return
	}
	if workers <= 0 {
		workers = 4
	}
	if workers > 8 {
		workers = 8
	}
	jobs := make(chan string, len(urls))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rawURL := range jobs {
				_, _ = f.ResolveImage(rawURL)
			}
		}()
	}
	for _, rawURL := range urls {
		jobs <- rawURL
	}
	close(jobs)
	wg.Wait()
}

func downloadAsset(client *http.Client, rawURL, referer string) ([]byte, string, string, error) {
	rawURL = normalizeAssetURL(rawURL, referer)
	if rawURL == "" {
		return nil, "", "", fmt.Errorf("empty asset url")
	}
	referers := buildDownloadRefererCandidates(rawURL, referer)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		for _, candidate := range referers {
			data, mediaType, finalURL, err := downloadAssetOnce(client, rawURL, candidate)
			if err == nil {
				return data, mediaType, finalURL, nil
			}
			lastErr = err
		}
		time.Sleep(time.Duration(120*(attempt+1)) * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("failed to download asset: %s", rawURL)
	}
	return nil, "", rawURL, lastErr
}

func buildDownloadRefererCandidates(rawURL, referer string) []string {
	candidates := make([]string, 0, 4)
	push := func(value string) {
		value = strings.TrimSpace(value)
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}
	if strings.TrimSpace(referer) != "" {
		push(referer)
	}
	if parsed, err := neturl.Parse(rawURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		push(parsed.Scheme + "://" + parsed.Host + "/")
	}
	push("")
	return candidates
}

func downloadAssetOnce(client *http.Client, rawURL, referer string) ([]byte, string, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", rawURL, err
	}
	req.Header.Set("User-Agent", defaultAssetUserAgent)
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp,image/*,*/*;q=0.8")
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", strings.TrimSpace(referer))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", rawURL, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", rawURL, fmt.Errorf("http %d for %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", rawURL, err
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	data, mediaType, err = normalizeAssetData(data, mediaType, resp.Request.URL.String())
	if err != nil {
		return nil, "", rawURL, err
	}
	return data, mediaType, resp.Request.URL.String(), nil
}

func normalizeAssetURL(rawURL, referer string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "/") {
		if parsed, err := neturl.Parse(strings.TrimSpace(referer)); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host + rawURL
		}
	}
	return rawURL
}

func normalizeAssetData(data []byte, mediaType, rawURL string) ([]byte, string, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if !shouldTranscodeToPNG(mediaType, rawURL) {
		return data, mediaType, nil
	}

	img, err := xwebp.Decode(bytes.NewReader(data))
	if err != nil {
		// Keep original bytes when transcoding fails to avoid dropping chapter images.
		return data, mediaType, nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/png", nil
}

func transcodeRasterToJPEG(data []byte, mediaType string) ([]byte, string, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if !strings.HasPrefix(mediaType, "image/") || mediaType == "image/svg+xml" {
		return data, mediaType, nil
	}
	img, err := decodeRasterImage(data, mediaType)
	if err != nil {
		return nil, "", err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 88}); err != nil {
		return data, mediaType, nil
	}
	if out.Len() == 0 {
		return nil, "", fmt.Errorf("jpeg encode produced empty output")
	}
	return out.Bytes(), "image/jpeg", nil
}

func shouldTranscodeToPNG(mediaType, rawURL string) bool {
	if strings.EqualFold(strings.TrimSpace(mediaType), "image/webp") {
		return true
	}
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(path.Ext(parsed.Path), ".webp")
}

func assetExtension(mediaType, rawURL string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png", "image/gif", "image/webp", "image/avif", "image/svg+xml":
		return ".jpg"
	}
	if parsed, err := neturl.Parse(rawURL); err == nil {
		if ext := strings.ToLower(path.Ext(parsed.Path)); ext != "" {
			switch ext {
			case ".jpeg":
				return ".jpg"
			case ".webp", ".png", ".gif":
				return ".jpg"
			case ".jpg", ".svg":
				return ext
			}
			if mime.TypeByExtension(ext) != "" {
				return ext
			}
		}
	}
	return ".jpg"
}

func assetMediaType(mediaType, ext string) string {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "image/jpeg" || mediaType == "image/jpg" {
		return mediaType
	}
	_ = ext
	return "image/jpeg"
}

func optimizeImageForEPUB(data []byte, mediaType string, aggressiveES bool) ([]byte, string, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if !aggressiveES {
		return data, mediaType, nil
	}
	if !strings.HasPrefix(mediaType, "image/") || mediaType == "image/gif" || mediaType == "image/svg+xml" {
		return data, mediaType, nil
	}
	if len(data) < 450*1024 {
		return data, mediaType, nil
	}

	img, err := decodeRasterImage(data, mediaType)
	if err != nil {
		return data, mediaType, nil
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return data, mediaType, nil
	}

	const maxWidth = 1400
	target := img
	if width > maxWidth {
		ratio := float64(maxWidth) / float64(width)
		targetHeight := int(math.Round(float64(height) * ratio))
		if targetHeight < 1 {
			targetHeight = 1
		}
		rgba := image.NewRGBA(image.Rect(0, 0, maxWidth, targetHeight))
		xdraw.ApproxBiLinear.Scale(rgba, rgba.Bounds(), img, bounds, xdraw.Over, nil)
		target = rgba
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, target, &jpeg.Options{Quality: 80}); err != nil {
		return data, mediaType, nil
	}
	if out.Len() >= len(data) {
		return data, mediaType, nil
	}
	return out.Bytes(), "image/jpeg", nil
}

func decodeRasterImage(data []byte, mediaType string) (image.Image, error) {
	switch mediaType {
	case "image/jpeg", "image/jpg":
		return jpeg.Decode(bytes.NewReader(data))
	case "image/png":
		return png.Decode(bytes.NewReader(data))
	case "image/webp":
		return xwebp.Decode(bytes.NewReader(data))
	default:
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
}

func writeStoredFile(zw *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	header.Modified = time.Now().UTC()
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o644)
	header.Modified = time.Now().UTC()
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func writeZipDir(zw *zip.Writer, name string) error {
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(os.ModeDir | 0o755)
	header.Modified = time.Now().UTC()
	_, err := zw.CreateHeader(header)
	return err
}

func fallback(value, other string) string {
	if strings.TrimSpace(value) == "" {
		return other
	}
	return value
}

func sanitize(value string) string {
	value = regexp.MustCompile(`[\\/:*?"<>|]+`).ReplaceAllString(value, "_")
	value = strings.TrimSpace(value)
	if value == "" {
		return "book"
	}
	return value
}

func escapeHTML(value string) string {
	return template.HTMLEscapeString(value)
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

const esjContainerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
	<rootfiles>
		<rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
	</rootfiles>
</container>`

const esjContentOPFTemplate = "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n" +
	"        <package xmlns=\"http://www.idpf.org/2007/opf\" unique-identifier=\"BookId\" version=\"3.0\">\n" +
	"          <metadata xmlns:dc=\"http://purl.org/dc/elements/1.1/\" xmlns:opf=\"http://www.idpf.org/2007/opf\">\n" +
	"            <dc:title>%s</dc:title>\n" +
	"            <dc:language>zh-CN</dc:language>\n" +
	"            <dc:identifier id=\"BookId\">%s</dc:identifier>\n" +
	"            <dc:creator>%s</dc:creator>\n" +
	"            <dc:date>%s</dc:date>\n" +
	"            %s\n" +
	"          </metadata>\n" +
	"          <manifest>\n" +
	"            %s\n" +
	"          </manifest>\n" +
	"          <spine>\n" +
	"            %s\n" +
	"          </spine>\n" +
	"        </package>"

const esjNavTemplate = `<?xml version="1.0" encoding="utf-8"?>
        <html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh">
          <head><title>目录</title></head>
          <body>
            <nav epub:type="toc" id="toc">
              <h1>目录</h1>
              <ol>
        %s</ol></nav></body></html>`

const contentOPFTemplate = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="BookId" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="BookId">urn:uuid:%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:creator>%s</dc:creator>
    <dc:language>zh-CN</dc:language>
		<dc:date>%s</dc:date>
		%s
  </metadata>
  <manifest>
    %s
  </manifest>
	<spine>
    %s
  </spine>
</package>`

const navTemplate = `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>%s</title><link rel="stylesheet" type="text/css" href="styles.css"/></head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>%s</h1>
    <ol>
      %s
    </ol>
  </nav>
</body>
</html>`

const ncxTemplate = `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE ncx PUBLIC "-//NISO//DTD ncx 2005-1//EN"
  "http://www.daisy.org/z3986/2005/ncx-2005-1.dtd">
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="%s"/>
  </head>
  <docTitle><text>%s</text></docTitle>
  <navMap>
    %s
  </navMap>
</ncx>`

const esjEPUBParagraphCSS = `.novel-paragraph{margin:0;line-height:1.9;text-indent:2em;}.novel-paragraph-first{text-indent:0;}.novel-paragraph + .novel-paragraph{margin-top:3.8em;}`

const defaultEPUBCSS = `body{font-family:Georgia,serif;line-height:1.9;margin:5%;}h1,h2{line-height:1.3;}article{page-break-after:always;}.novel-paragraph{margin:0;line-height:1.9;text-indent:2em;}.novel-paragraph-first{text-indent:0;}.novel-paragraph + .novel-paragraph{margin-top:3.8em;}.cover{margin-top:12%;text-align:center;}.cover-art,.illustration{margin:1.5em auto;text-align:center;text-indent:0;}.cover-art img,.illustration img{height:auto;max-width:100%;}.cover-art img{max-height:70vh;}.author{font-style:italic;}`
