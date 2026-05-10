package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

func TestNovelpiaSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/keyword/date/1/VTuber" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`<html><body>
<div class="col-md-12 novelbox mobile_hidden">
<table><tr><td onclick="$('.loads').show();location='/novel/2393';"><img src="//img.example/cover.jpg"></td>
<td><b onclick="$('.loads').show();location='/novel/2393';">言語チート転生 VTuber</b></td></tr></table>
</div>
<div class="col-md-12 novelbox mobile_hidden">
<table><tr><td onclick="$('.loads').show();location='/novel/6428';"></td>
<td><b onclick="$('.loads').show();location='/novel/6428';">Another VTuber</b></td></tr></table>
</div>
</body></html>`))
	}))
	defer server.Close()

	s := NewNovelpiaSite(config.DefaultConfig().ResolveSiteConfig("novelpia"))
	s.baseURL = server.URL
	s.client = server.Client()

	results, err := s.Search(context.Background(), "VTuber", 1)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].BookID != "2393" || !strings.Contains(results[0].Title, "VTuber") || results[0].CoverURL != "https://img.example/cover.jpg" {
		t.Fatalf("unexpected results: %#v", results)
	}
}
