package web

import (
	"strings"
	"testing"
)

func TestDiscoverySiteTabsUseChineseShortLabels(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`qidian: "起点"`,
		`ciweimao: "刺猬猫"`,
		`faloo: "飞卢"`,
		`fanqienovel: "番茄"`,
		`shuhuangdian: "书荒殿"`,
		`n17k: "17K"`,
		`sfacg: "SF轻文"`,
		`ciyuanji: "次元姬"`,
		`kadokado: "角角者"`,
		`alphapolis: "阿尔法"`,
		`button.title = source.name || sourceLabel(source.site)`,
		`name.textContent = discoverSiteShortLabel(source.site, source.name || sourceLabel(source.site))`,
		`api/discovery/trending?limit=30`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected discovery tab script to contain %s", needle)
		}
	}
}

func TestDiscoveryListUIPaginates(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`url.searchParams.set("page", String(requestedPage))`,
		`url.searchParams.set("page_size", String(pageSize))`,
		`function renderDiscoveryBookPagination()`,
		`discover.activeHasMore = Boolean(payload.has_more)`,
		`const rankOffset = Math.max(0, (discover.activePage - 1) * discover.activePageSize);`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected discovery pagination script to contain %s", needle)
		}
	}
}

func TestFavoritesTabPrefetchesHistoryCount(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`void refreshHistoryCount();`,
		`params.set("page_size", "1");`,
		`historyTabCountNode.textContent = String(total);`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected history badge prefetch to contain %s", needle)
		}
	}
}

func TestDiscoveryURLRestoresDirectQidianBooklists(t *testing.T) {
	data, err := templateFS.ReadFile("templates/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	script := string(data)
	for _, needle := range []string{
		`/^booklist:\d+$/.test(listID)`,
		"name: `书单 #${rawID}`",
		`await openDiscoverList(appState.discover.activeSite, list, urlPositiveInt(params, "page", 1))`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected direct discovery URL restore to contain %s", needle)
		}
	}
}
