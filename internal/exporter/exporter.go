package exporter

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	"html/template"
	"image/png"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
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
	NCX          string
	CoverPage    string
	ChapterFiles map[string]string
	Assets       []*epubAsset
}

type chapterBlock struct {
	Paragraph string
	ImageURL  string
}

type epubAssetFetcher struct {
	client  *http.Client
	assets  []*epubAsset
	byURL   map[string]*epubAsset
	counter int
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
		fmt.Fprintf(&buf, "\n\n# %s\n\n%s\n", chapter.Title, chapter.Content)
	}

	return buf.Bytes()
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
	if err := writeZipFile(zw, "OEBPS/toc.ncx", []byte(pkg.NCX)); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "OEBPS/styles.css", []byte(defaultEPUBCSS)); err != nil {
		_ = zw.Close()
		return err
	}
	if err := writeZipFile(zw, "OEBPS/cover.xhtml", []byte(pkg.CoverPage)); err != nil {
		_ = zw.Close()
		return err
	}
	for _, asset := range pkg.Assets {
		if err := writeZipFile(zw, "OEBPS/"+asset.Href, asset.Data); err != nil {
			_ = zw.Close()
			return err
		}
	}
	for name, body := range pkg.ChapterFiles {
		if err := writeZipFile(zw, "OEBPS/"+name, []byte(body)); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func buildEPUBContent(book *model.Book) (*epubPackage, error) {
	fetcher := newEPUBAssetFetcher()
	manifestItems := []string{
		`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`,
		`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`,
		`<item id="css" href="styles.css" media-type="text/css"/>`,
		`<item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/>`,
	}
	spineItems := []string{`<itemref idref="cover"/>`}
	navPoints := []string{`<li><a href="cover.xhtml">Cover</a></li>`}
	ncxPoints := []string{`<navPoint id="nav-cover" playOrder="1"><navLabel><text>Cover</text></navLabel><content src="cover.xhtml"/></navPoint>`}
	chapterFiles := make(map[string]string, len(book.Chapters))

	coverImageHref := ""
	if asset, err := fetcher.ResolveImage(book.CoverURL); err == nil && asset != nil {
		coverImageHref = asset.Href
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="%s" properties="cover-image"/>`, asset.ID, asset.Href, asset.MediaType))
	}

	for idx, chapter := range book.Chapters {
		fileName := fmt.Sprintf("chapter-%03d.xhtml", idx+1)
		itemID := fmt.Sprintf("chapter-%03d", idx+1)
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="application/xhtml+xml"/>`, itemID, fileName))
		spineItems = append(spineItems, fmt.Sprintf(`<itemref idref="%s"/>`, itemID))
		navPoints = append(navPoints, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, fileName, escapeHTML(chapter.Title)))
		ncxPoints = append(ncxPoints, fmt.Sprintf(`<navPoint id="nav-%03d" playOrder="%d"><navLabel><text>%s</text></navLabel><content src="%s"/></navPoint>`, idx+1, idx+2, escapeHTML(chapter.Title), fileName))
		chapterFiles[fileName] = buildChapterPage(book.Title, chapter, fetcher)
	}

	for _, asset := range fetcher.Assets() {
		if asset.Href == coverImageHref {
			continue
		}
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="%s"/>`, asset.ID, asset.Href, asset.MediaType))
	}

	bookUUID := makeBookUUID(book)
	opf := fmt.Sprintf(contentOPFTemplate,
		bookUUID,
		escapeHTML(fallback(book.Title, book.ID)),
		escapeHTML(fallback(book.Author, "unknown")),
		strings.Join(manifestItems, "\n    "),
		strings.Join(spineItems, "\n    "),
	)
	navTitle := escapeHTML(fallback(book.Title, book.ID))
	nav := fmt.Sprintf(navTemplate, navTitle, navTitle, strings.Join(navPoints, "\n      "))
	ncx := fmt.Sprintf(ncxTemplate, "urn:uuid:"+bookUUID, navTitle, strings.Join(ncxPoints, "\n    "))
	return &epubPackage{
		OPF:          opf,
		Nav:          nav,
		NCX:          ncx,
		CoverPage:    buildCoverPage(fallback(book.Title, book.ID), book.Author, book.Description, coverImageHref),
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

func buildChapterPage(bookTitle string, chapter model.Chapter, fetcher *epubAssetFetcher) string {
	blocks := parseChapterBlocks(chapter.Content)
	body := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.ImageURL == "" {
			body = append(body, "<p>"+escapeHTML(strings.ReplaceAll(block.Paragraph, "\n", " "))+"</p>")
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

func parseChapterBlocks(content string) []chapterBlock {
	parts := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n\n")
	blocks := make([]chapterBlock, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if imageURL := parseImagePlaceholder(part); imageURL != "" {
			blocks = append(blocks, chapterBlock{ImageURL: imageURL})
			continue
		}
		blocks = append(blocks, chapterBlock{Paragraph: part})
	}
	return blocks
}

func parseImagePlaceholder(value string) string {
	match := chapterImageRe.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func newEPUBAssetFetcher() *epubAssetFetcher {
	return &epubAssetFetcher{
		client: &http.Client{Timeout: 45 * time.Second},
		byURL:  make(map[string]*epubAsset),
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
	if asset, ok := f.byURL[rawURL]; ok {
		return asset, nil
	}

	data, mediaType, finalURL, err := downloadAsset(f.client, rawURL)
	if err != nil {
		return nil, err
	}
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
	return asset, nil
}

func downloadAsset(client *http.Client, rawURL string) ([]byte, string, string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", rawURL, err
	}
	req.Header.Set("User-Agent", defaultAssetUserAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if parsed, err := neturl.Parse(rawURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		req.Header.Set("Referer", parsed.Scheme+"://"+parsed.Host+"/")
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
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	}
	if parsed, err := neturl.Parse(rawURL); err == nil {
		if ext := strings.ToLower(path.Ext(parsed.Path)); ext != "" {
			switch ext {
			case ".jpeg":
				return ".jpg"
			case ".webp":
				return ".png"
			case ".jpg", ".png", ".gif", ".svg":
				return ext
			}
			if mime.TypeByExtension(ext) != "" {
				return ext
			}
		}
	}
	return ".img"
}

func assetMediaType(mediaType, ext string) string {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if strings.HasPrefix(mediaType, "image/") {
		return mediaType
	}
	if guessed := mime.TypeByExtension(ext); guessed != "" {
		return guessed
	}
	return "application/octet-stream"
}

func writeStoredFile(zw *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
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

const contentOPFTemplate = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="BookId" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="BookId">urn:uuid:%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:creator>%s</dc:creator>
    <dc:language>zh-CN</dc:language>
  </metadata>
  <manifest>
    %s
  </manifest>
  <spine toc="ncx">
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

const defaultEPUBCSS = `body{font-family:Georgia,serif;line-height:1.8;margin:5%%;}h1,h2{line-height:1.3;}article{page-break-after:always;}p{text-indent:2em;}.cover{margin-top:12%%;text-align:center;}.cover-art,.illustration{margin:1.5em auto;text-align:center;text-indent:0;}.cover-art img,.illustration img{height:auto;max-width:100%%;}.cover-art img{max-height:70vh;}.author{font-style:italic;}`
