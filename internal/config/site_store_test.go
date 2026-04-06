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
