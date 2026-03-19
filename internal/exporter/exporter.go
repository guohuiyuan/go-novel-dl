package exporter

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type Service struct{}

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
	defer zw.Close()

	if err := writeStoredFile(zw, "mimetype", []byte("application/epub+zip")); err != nil {
		return err
	}
	if err := writeZipFile(zw, "META-INF/container.xml", []byte(containerXML)); err != nil {
		return err
	}

	bookTitle := fallback(book.Title, book.ID)
	opf, nav, chapterFiles, err := buildEPUBContent(book)
	if err != nil {
		return err
	}
	if err := writeZipFile(zw, "OEBPS/content.opf", []byte(opf)); err != nil {
		return err
	}
	if err := writeZipFile(zw, "OEBPS/nav.xhtml", []byte(nav)); err != nil {
		return err
	}
	if err := writeZipFile(zw, "OEBPS/styles.css", []byte(defaultEPUBCSS)); err != nil {
		return err
	}
	if err := writeZipFile(zw, "OEBPS/cover.xhtml", []byte(buildCoverPage(bookTitle, book.Author, book.Description))); err != nil {
		return err
	}
	for name, body := range chapterFiles {
		if err := writeZipFile(zw, "OEBPS/"+name, []byte(body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

func buildEPUBContent(book *model.Book) (string, string, map[string]string, error) {
	manifestItems := []string{
		`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`,
		`<item id="css" href="styles.css" media-type="text/css"/>`,
		`<item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/>`,
	}
	spineItems := []string{`<itemref idref="cover"/>`}
	navPoints := []string{`<li><a href="cover.xhtml">Cover</a></li>`}
	chapterFiles := make(map[string]string, len(book.Chapters))
	for idx, chapter := range book.Chapters {
		fileName := fmt.Sprintf("chapter-%03d.xhtml", idx+1)
		itemID := fmt.Sprintf("chapter-%03d", idx+1)
		manifestItems = append(manifestItems, fmt.Sprintf(`<item id="%s" href="%s" media-type="application/xhtml+xml"/>`, itemID, fileName))
		spineItems = append(spineItems, fmt.Sprintf(`<itemref idref="%s"/>`, itemID))
		navPoints = append(navPoints, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, fileName, escapeHTML(chapter.Title)))
		chapterFiles[fileName] = buildChapterPage(book.Title, chapter)
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
	return opf, nav, chapterFiles, nil
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

func buildCoverPage(title, author, description string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>%s</title><link rel="stylesheet" type="text/css" href="styles.css"/></head>
<body><section class="cover"><h1>%s</h1><p class="author">%s</p><p>%s</p></section></body>
</html>`, escapeHTML(title), escapeHTML(title), escapeHTML(author), escapeHTML(description))
}

func buildChapterPage(bookTitle string, chapter model.Chapter) string {
	paragraphs := strings.Split(strings.ReplaceAll(chapter.Content, "\r\n", "\n"), "\n\n")
	body := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		body = append(body, "<p>"+escapeHTML(strings.ReplaceAll(paragraph, "\n", " "))+"</p>")
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>%s - %s</title><link rel="stylesheet" type="text/css" href="styles.css"/></head>
<body><article><h2>%s</h2>%s</article></body>
</html>`, escapeHTML(bookTitle), escapeHTML(chapter.Title), escapeHTML(chapter.Title), strings.Join(body, ""))
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

const defaultEPUBCSS = `body{font-family:Georgia,serif;line-height:1.8;margin:5%%;}h1,h2{line-height:1.3;}article{page-break-after:always;}p{text-indent:2em;} .cover{margin-top:25%%;text-align:center;} .author{font-style:italic;}`
