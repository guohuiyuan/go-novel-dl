package progress

import "testing"

func TestRenderBar(t *testing.T) {
	bar := renderBar(5, 10, 10)
	if bar != "[#####-----]" {
		t.Fatalf("unexpected bar: %s", bar)
	}
}
