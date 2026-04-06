package site

import (
	"strings"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

type SiteDescriptor struct {
	Key              string       `json:"key"`
	DisplayName      string       `json:"display_name"`
	Tags             []string     `json:"tags,omitempty"`
	Capabilities     Capabilities `json:"capabilities"`
	DefaultAvailable bool         `json:"default_available"`
}

var defaultAvailableSiteKeys = []string{
	"linovelib",
	"n23qb",
	"ixdzs8",
	"ruochu",
	"fanqienovel",
	"sfacg",
	"ciyuanji",
	"ciweimao",
	"n8novel",
	"shuhaige",
	"alicesw",
}

func DefaultAvailableSiteKeys() []string {
	keys := make([]string, len(defaultAvailableSiteKeys))
	copy(keys, defaultAvailableSiteKeys)
	return keys
}

func DefaultAvailableSiteSet() map[string]struct{} {
	set := make(map[string]struct{}, len(defaultAvailableSiteKeys))
	for _, key := range defaultAvailableSiteKeys {
		set[key] = struct{}{}
	}
	return set
}

func (r *Registry) SearchableKeys() []string {
	return r.filterKeysByCapability(func(capabilities Capabilities) bool {
		return capabilities.Search
	})
}

func (r *Registry) DownloadableKeys() []string {
	return r.filterKeysByCapability(func(capabilities Capabilities) bool {
		return capabilities.Download
	})
}

func (r *Registry) DefaultSearchKeys() []string {
	set := DefaultAvailableSiteSet()
	keys := make([]string, 0, len(defaultAvailableSiteKeys))
	for _, key := range defaultAvailableSiteKeys {
		if _, ok := set[key]; !ok {
			continue
		}
		descriptor, ok := r.SiteDescriptor(key)
		if !ok || !descriptor.Capabilities.Search {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func (r *Registry) DefaultDownloadKeys() []string {
	set := DefaultAvailableSiteSet()
	keys := make([]string, 0, len(defaultAvailableSiteKeys))
	for _, key := range defaultAvailableSiteKeys {
		if _, ok := set[key]; !ok {
			continue
		}
		descriptor, ok := r.SiteDescriptor(key)
		if !ok || !descriptor.Capabilities.Download {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func (r *Registry) AllSiteDescriptors() []SiteDescriptor {
	return r.SiteDescriptors(nil)
}

func (r *Registry) SiteDescriptors(keys []string) []SiteDescriptor {
	orderedKeys := keys
	if len(orderedKeys) == 0 {
		orderedKeys = r.Keys()
	}

	descriptors := make([]SiteDescriptor, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		descriptor, ok := r.SiteDescriptor(key)
		if !ok {
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

func (r *Registry) SiteDescriptor(key string) (SiteDescriptor, bool) {
	site, err := r.Build(key, configForDescriptor(key))
	if err != nil {
		return SiteDescriptor{}, false
	}

	metadata := descriptorMetadata(key)
	displayName := strings.TrimSpace(metadata.Title)
	if displayName == "" {
		displayName = site.DisplayName()
	}

	_, isDefault := DefaultAvailableSiteSet()[key]
	return SiteDescriptor{
		Key:              key,
		DisplayName:      displayName,
		Tags:             metadata.Tags,
		Capabilities:     site.Capabilities(),
		DefaultAvailable: isDefault,
	}, true
}

func (r *Registry) filterKeysByCapability(accept func(Capabilities) bool) []string {
	keys := make([]string, 0, len(r.factories))
	for _, key := range r.Keys() {
		site, err := r.Build(key, configForDescriptor(key))
		if err != nil {
			continue
		}
		if !accept(site.Capabilities()) {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func configForDescriptor(key string) config.ResolvedSiteConfig {
	defaults := config.DefaultConfig()
	return defaults.ResolveSiteConfig(key)
}
