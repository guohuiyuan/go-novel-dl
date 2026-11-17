//go:build !android && !ios

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "desktop_app is the mobile NovelDL shell; run ./cmd/novel-dl web or build the desktop package on desktop platforms")
	os.Exit(2)
}
