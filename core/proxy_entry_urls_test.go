package core

import "testing"

func TestParseProxyEntryURLsTagsEachEntry(t *testing.T) {
	entries, err := ParseProxyEntryURLs(
		"http://u:p@host:8080|google,us; http://u:p@host:8081|google ;http://u:p@host:8082",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].URL != "http://u:p@host:8080" {
		t.Fatalf("first url = %q", entries[0].URL)
	}
	if len(entries[0].Tags) != 2 || entries[0].Tags[0] != "google" || entries[0].Tags[1] != "us" {
		t.Fatalf("first tags = %v", entries[0].Tags)
	}
	// Surrounding whitespace must not become part of the URL or the tag.
	if entries[1].URL != "http://u:p@host:8081" || entries[1].Tags[0] != "google" {
		t.Fatalf("second entry = %+v", entries[1])
	}
	// No "|" means the default pool, exactly like an untagged entries item.
	if entries[2].URL != "http://u:p@host:8082" || len(entries[2].Tags) != 0 {
		t.Fatalf("third entry = %+v", entries[2])
	}
}

func TestParseProxyEntryURLsIgnoresEmptySegments(t *testing.T) {
	// A trailing separator, or an unset variable, must not invent an entry.
	for _, raw := range []string{"", "   ", ";", "; ;"} {
		entries, err := ParseProxyEntryURLs(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if len(entries) != 0 {
			t.Fatalf("parse %q gave %d entries, want 0", raw, len(entries))
		}
	}
}

func TestParseProxyEntryURLsRejectsATagWithoutAURL(t *testing.T) {
	if _, err := ParseProxyEntryURLs("|google"); err == nil {
		t.Fatal("a tag with no URL should be rejected")
	}
}

func TestNormalizeProxiesConfigMergesEntryURLs(t *testing.T) {
	cfg, err := NormalizeProxiesConfig(ProxiesConfig{
		Entries:   []ProxyEntryConfig{{URL: "http://u:p@host:8080", Tags: []string{"us"}}},
		EntryURLs: "http://u:p@host:8080|google;http://u:p@host:8081|google",
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(cfg.Entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(cfg.Entries), cfg.Entries)
	}
	// The same URL declared both ways is one entry carrying both tags.
	first := cfg.Entries[0]
	if first.URL != "http://u:p@host:8080" || len(first.Tags) != 2 {
		t.Fatalf("merged entry = %+v", first)
	}
}

func TestNormalizeProxiesConfigRejectsAMalformedEntryURL(t *testing.T) {
	if _, err := NormalizeProxiesConfig(ProxiesConfig{EntryURLs: "|google"}); err == nil {
		t.Fatal("a malformed entry_urls value should fail configuration")
	}
}
