package main

import (
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	ico "github.com/Kodeworks/golang-image-ico"
	"golang.org/x/image/draw"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("get wd: %v", err)
	}

	var srcImg image.Image
	var loadedPath string
	candidates := []string{
		filepath.Join(root, "logo.png"),
		filepath.Join(root, "winres", "icon_256x256.png"),
	}
	for _, srcPath := range candidates {
		srcFile, openErr := os.Open(srcPath)
		if openErr != nil {
			continue
		}

		decoded, _, decodeErr := image.Decode(srcFile)
		srcFile.Close()
		if decodeErr != nil {
			log.Fatalf("decode %s: %v", srcPath, decodeErr)
		}

		srcImg = decoded
		loadedPath = srcPath
		break
	}
	if srcImg == nil {
		log.Fatalf("no icon source found, expected one of: %s or %s", candidates[0], candidates[1])
	}
	log.Printf("using icon source: %s", loadedPath)

	dst := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.CatmullRom.Scale(dst, dst.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	writePNG(filepath.Join(root, "logo_256x256.png"), dst)
	writePNG(filepath.Join(root, "internal", "web", "templates", "icon-256.png"), dst)
	writePNG(filepath.Join(root, "winres", "icon_256x256.png"), dst)
	writePNG(filepath.Join(root, "desktop", "icon.png"), dst)
	writeICO(filepath.Join(root, "winres", "icon_256x256.ico"), dst)
	writeICO(filepath.Join(root, "desktop", "icon.ico"), dst)
}

func writePNG(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		log.Fatalf("encode png %s: %v", path, err)
	}
}

func writeICO(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	file, err := os.Create(path)
	if err != nil {
		log.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()

	if err := ico.Encode(file, img); err != nil {
		log.Fatalf("encode ico %s: %v", path, err)
	}
}
