package config

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestGeneralConfigBlurWebImagesRoundTrip(t *testing.T) {
	resetSiteCatalogForTest(t)

	record, err := LoadGeneralConfig()
	if err != nil {
		t.Fatalf("load default general config: %v", err)
	}
	if record.BlurWebImages {
		t.Fatalf("expected blur_web_images default false")
	}

	record.BlurWebImages = true
	saved, err := SaveGeneralConfig(record)
	if err != nil {
		t.Fatalf("save general config: %v", err)
	}
	if !saved.BlurWebImages {
		t.Fatalf("expected saved blur_web_images true")
	}

	loaded, err := LoadGeneralConfig()
	if err != nil {
		t.Fatalf("reload general config: %v", err)
	}
	if !loaded.BlurWebImages {
		t.Fatalf("expected reloaded blur_web_images true")
	}

	cfg := DefaultConfig()
	if err := mergeGeneralConfig(&cfg); err != nil {
		t.Fatalf("merge general config: %v", err)
	}
	if !cfg.General.BlurWebImages {
		t.Fatalf("expected merged config to enable blur_web_images")
	}
}

func TestGeneralConfigDisableCacheRoundTrip(t *testing.T) {
	resetSiteCatalogForTest(t)

	record, err := LoadGeneralConfig()
	if err != nil {
		t.Fatalf("load default general config: %v", err)
	}
	if record.DisableCache {
		t.Fatalf("expected disable_cache default false")
	}

	record.DisableCache = true
	saved, err := SaveGeneralConfig(record)
	if err != nil {
		t.Fatalf("save general config: %v", err)
	}
	if !saved.DisableCache {
		t.Fatalf("expected saved disable_cache true")
	}

	loaded, err := LoadGeneralConfig()
	if err != nil {
		t.Fatalf("reload general config: %v", err)
	}
	if !loaded.DisableCache {
		t.Fatalf("expected reloaded disable_cache true")
	}

	cfg := DefaultConfig()
	if err := mergeGeneralConfig(&cfg); err != nil {
		t.Fatalf("merge general config: %v", err)
	}
	if !cfg.General.DisableCache {
		t.Fatalf("expected merged config to disable cache")
	}
}

func TestESJZoneLoginRequiredIsNotForced(t *testing.T) {
	resetSiteCatalogForTest(t)

	cfg := DefaultConfig()
	if cfg.ResolveSiteConfig("esjzone").General.LoginRequired {
		t.Fatalf("expected esjzone default login_required false")
	}

	disabled := false
	item, err := UpsertSiteCatalog("esjzone", SiteCatalogUpdate{LoginRequired: &disabled})
	if err != nil {
		t.Fatalf("update esjzone login_required: %v", err)
	}
	if item.LoginRequired {
		t.Fatalf("expected esjzone site catalog to allow login_required=false")
	}

	if err := mergeSiteCatalog(&cfg); err != nil {
		t.Fatalf("merge site catalog: %v", err)
	}
	if cfg.ResolveSiteConfig("esjzone").General.LoginRequired {
		t.Fatalf("expected merged esjzone config to keep login_required=false")
	}
}

func TestLegacyESJZoneLoginRequirementIsRelaxedWhenNoAuthConfigured(t *testing.T) {
	resetSiteCatalogForTest(t)

	if err := ensureSiteCatalogDB(); err != nil {
		t.Fatalf("ensure site catalog: %v", err)
	}
	if err := siteCatalogDB.Model(&siteCatalogEntry{}).
		Where("key = ?", "esjzone").
		Updates(map[string]any{
			"login_required": true,
			"username":       "",
			"password":       "",
			"cookie":         "",
		}).Error; err != nil {
		t.Fatalf("seed legacy esjzone row: %v", err)
	}
	if err := relaxLegacyESJLoginRequirement(siteCatalogDB); err != nil {
		t.Fatalf("relax legacy esjzone row: %v", err)
	}

	item, err := UpsertSiteCatalog("esjzone", SiteCatalogUpdate{})
	if err != nil {
		t.Fatalf("load esjzone row: %v", err)
	}
	if item.LoginRequired {
		t.Fatalf("expected legacy esjzone login_required to be relaxed")
	}
}

func resetSiteCatalogForTest(t *testing.T) {
	t.Helper()

	t.Setenv("NOVEL_DL_SITE_DB", filepath.Join(t.TempDir(), "site_catalog.db"))
	siteCatalogDB = nil
	siteCatalogErr = nil
	siteCatalogOnce = sync.Once{}

	t.Cleanup(func() {
		if siteCatalogDB != nil {
			if sqlDB, err := siteCatalogDB.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		siteCatalogDB = nil
		siteCatalogErr = nil
		siteCatalogOnce = sync.Once{}
	})
}
