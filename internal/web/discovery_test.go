package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
	"github.com/guohuiyuan/go-novel-dl/internal/model"
	"github.com/guohuiyuan/go-novel-dl/internal/site"
)

type fakeDiscoveryWebSite struct {
	fakeWebSite
	lists    []site.DiscoveryList
	books    map[string][]model.SearchResult
	listsErr error
	booksErr error
}

type blockingDiscoveryWebSite struct {
	fakeDiscoveryWebSite
	started chan<- string
	release <-chan struct{}
}

func (s blockingDiscoveryWebSite) DiscoveryBooks(ctx context.Context, _ string, limit int) ([]model.SearchResult, error) {
	s.started <- s.key
	select {
	case <-s.release:
		return []model.SearchResult{{Site: s.key, BookID: s.key + "-1", Title: s.key}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s fakeDiscoveryWebSite) DiscoveryLists(ctx context.Context) ([]site.DiscoveryList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.listsErr != nil {
		return nil, s.listsErr
	}
	return append([]site.DiscoveryList(nil), s.lists...), nil
}

func (s fakeDiscoveryWebSite) DiscoveryBooks(ctx context.Context, listID string, limit int) ([]model.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.booksErr != nil {
		return nil, s.booksErr
	}
	items := append([]model.SearchResult(nil), s.books[listID]...)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func registerFakeDiscoverySite(
	service *Service,
	key string,
	name string,
	lists []site.DiscoveryList,
	books map[string][]model.SearchResult,
	listsErr error,
) {
	service.Runtime.Registry.Register(key, func(config.ResolvedSiteConfig) site.Site {
		return fakeDiscoveryWebSite{
			fakeWebSite: fakeWebSite{
				key:         key,
				displayName: name,
				capabilities: site.Capabilities{
					Download: true,
					Search:   true,
				},
			},
			lists:    lists,
			books:    books,
			listsErr: listsErr,
		}
	})
}

func discoveryTestGet(t *testing.T, service *Service, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp := httptest.NewRecorder()
	newRouter(service).ServeHTTP(resp, req)
	return resp
}

func TestDiscoveryCatalogKeepsHealthySitesWhenAnotherSiteFails(t *testing.T) {
	service := newTestService()
	registerFakeDiscoverySite(service, "rank-a", "榜单 A", []site.DiscoveryList{
		{ID: "hot", Name: "热门榜", Kind: "ranking"},
	}, nil, nil)
	registerFakeDiscoverySite(service, "rank-broken", "故障渠道", nil, nil, errors.New("temporary upstream failure"))

	resp := discoveryTestGet(t, service, RoutePrefix+"/api/discovery/catalog?sites=rank-a,rank-broken")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload discoveryCatalogResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if len(payload.Sites) != 1 || payload.Sites[0].Site != "rank-a" {
		t.Fatalf("expected one healthy site, got %+v", payload.Sites)
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0].Site != "rank-broken" {
		t.Fatalf("expected one warning for the failed site, got %+v", payload.Warnings)
	}
	if !strings.Contains(payload.Warnings[0].Error, "temporary upstream failure") {
		t.Fatalf("unexpected warning: %+v", payload.Warnings[0])
	}
}

func TestDiscoveryCatalogFiltersByKind(t *testing.T) {
	service := newTestService()
	registerFakeDiscoverySite(service, "rank-a", "榜单 A", []site.DiscoveryList{
		{ID: "hot", Name: "热门榜", Kind: "ranking"},
		{ID: "curated", Name: "编辑书单", Kind: "booklist"},
	}, nil, nil)

	resp := discoveryTestGet(t, service, RoutePrefix+"/api/discovery/catalog?kind=booklist&sites=rank-a")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload discoveryCatalogResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode catalog response: %v", err)
	}
	if len(payload.Sites) != 1 || len(payload.Sites[0].Lists) != 1 {
		t.Fatalf("expected one booklist, got %+v", payload.Sites)
	}
	if payload.Sites[0].Lists[0].ID != "curated" {
		t.Fatalf("unexpected filtered list: %+v", payload.Sites[0].Lists)
	}
}

func TestDiscoveryListReturnsSelectedBooks(t *testing.T) {
	service := newTestService()
	registerFakeDiscoverySite(service, "rank-a", "榜单 A", []site.DiscoveryList{
		{ID: "hot", Name: "热门榜", Description: "当前热门作品", Kind: "ranking"},
	}, map[string][]model.SearchResult{
		"hot": {
			{Site: "rank-a", BookID: "1", Title: "第一本"},
			{Site: "rank-a", BookID: "2", Title: "第二本"},
		},
	}, nil)

	resp := discoveryTestGet(t, service, RoutePrefix+"/api/discovery/list?site=rank-a&id=hot&limit=2")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload discoveryListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if payload.Site != "rank-a" || payload.Name != "榜单 A" || payload.List.ID != "hot" {
		t.Fatalf("unexpected list metadata: %+v", payload)
	}
	if len(payload.Books) != 2 || payload.Books[0].BookID != "1" || payload.Books[1].BookID != "2" {
		t.Fatalf("unexpected list books: %+v", payload.Books)
	}
}

func TestDiscoveryListAcceptsDirectBooklistWithoutCatalogMetadata(t *testing.T) {
	service := newTestService()
	registerFakeDiscoverySite(service, "qidian", "起点", nil, map[string][]model.SearchResult{
		"booklist:533326": {
			{Site: "qidian", BookID: "533326-1", Title: "旧链接仍可读取"},
		},
	}, nil)

	resp := discoveryTestGet(t, service, RoutePrefix+"/api/discovery/list?site=qidian&id=booklist%3A533326")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected direct booklist to be readable, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload discoveryListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if payload.List.ID != "booklist:533326" || payload.List.Name != "书单 #533326" || payload.List.Kind != "booklist" {
		t.Fatalf("unexpected fallback metadata: %+v", payload.List)
	}
	if len(payload.Books) != 1 || payload.Books[0].BookID != "533326-1" {
		t.Fatalf("unexpected direct booklist books: %+v", payload.Books)
	}
}

func TestDiscoveryListNotFound(t *testing.T) {
	service := newTestService()
	registerFakeDiscoverySite(service, "rank-a", "榜单 A", []site.DiscoveryList{
		{ID: "hot", Name: "热门榜", Kind: "ranking"},
	}, nil, nil)

	resp := discoveryTestGet(t, service, RoutePrefix+"/api/discovery/list?site=rank-a&id=missing")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestDiscoveryTrendingInterleavesSites(t *testing.T) {
	service := newTestService()
	registerFakeDiscoverySite(service, "rank-a", "榜单 A", []site.DiscoveryList{
		{ID: "hot", Name: "热门榜", Kind: "ranking"},
	}, map[string][]model.SearchResult{
		"hot": {
			{Site: "rank-a", BookID: "a1", Title: "A1"},
			{Site: "rank-a", BookID: "a2", Title: "A2"},
		},
	}, nil)
	registerFakeDiscoverySite(service, "rank-b", "榜单 B", []site.DiscoveryList{
		{ID: "hot", Name: "热门榜", Kind: "ranking"},
	}, map[string][]model.SearchResult{
		"hot": {
			{Site: "rank-b", BookID: "b1", Title: "B1"},
			{Site: "rank-b", BookID: "b2", Title: "B2"},
		},
	}, nil)

	resp := discoveryTestGet(t, service, RoutePrefix+"/api/discovery/trending?sites=rank-a,rank-b&limit=4")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var payload discoveryTrendingResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode trending response: %v", err)
	}
	got := make([]string, 0, len(payload.Books))
	for _, item := range payload.Books {
		got = append(got, item.BookID)
	}
	want := []string{"a1", "b1", "a2", "b2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected interleaved books %v, got %v", want, got)
	}
}

func TestDiscoveryTrendingRequestsSitesConcurrently(t *testing.T) {
	service := newTestService()
	started := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer unblock()

	for _, key := range []string{"parallel-a", "parallel-b"} {
		key := key
		service.Runtime.Registry.Register(key, func(config.ResolvedSiteConfig) site.Site {
			return blockingDiscoveryWebSite{
				fakeDiscoveryWebSite: fakeDiscoveryWebSite{
					fakeWebSite: fakeWebSite{key: key, displayName: key},
					lists:       []site.DiscoveryList{{ID: "hot", Name: "热门榜", Kind: "ranking"}},
				},
				started: started,
				release: release,
			}
		})
	}

	router := newRouter(service)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, RoutePrefix+"/api/discovery/trending?sites=parallel-a,parallel-b&limit=2", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		done <- resp
	}()

	for idx := 0; idx < 2; idx++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("discovery requests did not start concurrently")
		}
	}
	unblock()

	select {
	case resp := <-done:
		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent discovery request did not finish")
	}
}

func TestCollectDiscoveryTrendingResultsReturnsPartialAtDeadline(t *testing.T) {
	results := make(chan discoveryBooksResult, 2)
	results <- discoveryBooksResult{
		siteKey: "fast",
		books:   []model.SearchResult{{Site: "fast", BookID: "1", Title: "快速结果"}},
	}

	startedAt := time.Now()
	bySite, warnings := collectDiscoveryTrendingResults(
		context.Background(),
		[]string{"fast", "slow"},
		results,
		30*time.Millisecond,
	)
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("partial results took too long: %s", elapsed)
	}
	if len(bySite["fast"]) != 1 {
		t.Fatalf("expected fast site results, got %+v", bySite)
	}
	if _, ok := bySite["slow"]; ok {
		t.Fatalf("slow site should not delay the response: %+v", bySite)
	}
	if len(warnings) != 1 || warnings[0].Site != "slow" {
		t.Fatalf("expected one slow-site warning, got %+v", warnings)
	}
}

func TestDefaultDiscoverySitesExcludeUnavailableChannels(t *testing.T) {
	sites, explicit := normalizeDiscoverySites(nil)
	if explicit {
		t.Fatal("empty site list should use defaults")
	}
	foundQQReader := false
	foundWeRead := false
	for _, siteKey := range sites {
		if siteKey == "qqreader" {
			foundQQReader = true
		}
		if siteKey == "weread" {
			foundWeRead = true
		}
		if siteKey == "alphapolis" || siteKey == "kadokado" {
			t.Fatalf("%s must not be included in default discovery sites: %v", siteKey, sites)
		}
	}
	if !foundQQReader {
		t.Fatalf("qqreader should be included in default discovery sites: %v", sites)
	}
	if !foundWeRead {
		t.Fatalf("weread should be included in default discovery sites: %v", sites)
	}
}

func TestIndexPageIncludesDiscoveryControls(t *testing.T) {
	service := newTestService()
	resp := discoveryTestGet(t, service, RoutePrefix+"/")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, needle := range []string{
		`id="discoverTabButton"`,
		`id="discoverTabPanel"`,
		`id="discoverTrendingPanel"`,
		`id="discoverCatalogPanel"`,
		`id="discoverListPanel"`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected index page to contain %s", needle)
		}
	}
}
