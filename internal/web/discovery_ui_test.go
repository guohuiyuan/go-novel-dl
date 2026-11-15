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
		`button.title = source.name || sourceLabel(source.site)`,
		`name.textContent = discoverSiteShortLabel(source.site, source.name || sourceLabel(source.site))`,
		`api/discovery/trending?limit=30`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected discovery tab script to contain %s", needle)
		}
	}
	if strings.Contains(script, `alphapolis: "阿尔法"`) {
		t.Fatal("alphapolis must not be exposed as a discovery ranking site")
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
		`await openDiscoverList(appState.discover.activeSite, list)`,
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("expected direct discovery URL restore to contain %s", needle)
		}
	}
}
