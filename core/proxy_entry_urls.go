package core

import (
	"fmt"
	"strings"
)

// ParseProxyEntryURLs reads tagged proxy entries from a single string, so a
// deployment configured only through environment variables can define a proxy
// pool at all.
//
// `proxies.entries` is a list of objects, which viper cannot bind from one
// environment variable. On a platform that mounts no config file — Railway,
// Fly, a plain container — that left `proxies.global` as the only reachable
// setting, and a global proxy sends every engine through it even when most
// engines work better direct.
//
// The format is one entry per `;`, with optional comma-separated tags after a
// `|`:
//
//	http://user:pass@host:8080|google,us;http://user:pass@host:8081|google
//
// An entry with no tags joins the default pool, exactly as an untagged
// `proxies.entries` item does. Whitespace and empty segments are ignored so a
// trailing separator is harmless.
func ParseProxyEntryURLs(raw string) ([]ProxyEntryConfig, error) {
	entries := []ProxyEntryConfig{}
	for _, segment := range strings.Split(raw, ";") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}

		rawURL := segment
		var tags []string
		// A proxy URL has no "|", so the last one separates the tag list.
		if index := strings.LastIndex(segment, "|"); index >= 0 {
			rawURL = strings.TrimSpace(segment[:index])
			for _, tag := range strings.Split(segment[index+1:], ",") {
				if trimmed := strings.TrimSpace(tag); trimmed != "" {
					tags = append(tags, trimmed)
				}
			}
		}
		if rawURL == "" {
			return nil, fmt.Errorf("proxy entry %q has no URL", segment)
		}
		entries = append(entries, ProxyEntryConfig{URL: rawURL, Tags: tags})
	}
	return entries, nil
}
