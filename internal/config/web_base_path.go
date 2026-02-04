package config

import "strings"

// NormalizeWebBasePath returns a single-segment path without leading/trailing slashes.
// Empty or "/" returns "".
func NormalizeWebBasePath(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "/")
	return value
}

// WebBasePathPrefix returns the normalized base path with a leading slash.
// Empty returns "".
func WebBasePathPrefix() string {
	cfg := ReadConfig()
	base := NormalizeWebBasePath(cfg.System.WebBasePath)
	if base == "" {
		return ""
	}
	return "/" + base
}
