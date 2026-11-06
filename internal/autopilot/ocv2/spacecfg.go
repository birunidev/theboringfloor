package ocv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SpaceURL is the only Space MCP endpoint the autopilot accepts.
const SpaceURL = "https://space.birunidev.com/mcp"

type mcpEntry struct {
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled"`
}

// mcpEntries reads only the url/enabled fields of every MCP server in an
// opencode config file. It accepts both shapes seen in the wild:
// {"mcp": {"name": {...}}} and {"mcp": {"servers": {"name": {...}}}}.
// No other field (headers, tokens) is decoded.
func mcpEntries(path string) (map[string]mcpEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		MCP map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := map[string]mcpEntry{}
	for name, raw := range doc.MCP {
		if name == "servers" {
			var inner map[string]mcpEntry
			if json.Unmarshal(raw, &inner) == nil {
				for k, v := range inner {
					out[k] = v
				}
			}
			continue
		}
		var e mcpEntry
		if json.Unmarshal(raw, &e) == nil {
			out[name] = e
		}
	}
	return out, nil
}

// CheckSpaceConfig verifies, before any run, that opencode reaches Space
// only through birunidev-space at SpaceURL: home/opencode.json must define
// it with exactly that URL, and no enabled MCP entry in either config may
// point at another birunidev.com Space host (e.g. the legacy
// workspace.birunidev.com).
func CheckSpaceConfig(home string) error {
	project := filepath.Join(home, "opencode.json")
	global := filepath.Join(home, ".config", "opencode", "opencode.json")
	found := false
	for _, path := range []string{project, global} {
		entries, err := mcpEntries(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for name, e := range entries {
			enabled := e.Enabled == nil || *e.Enabled
			if name == SpaceServer {
				if e.URL != SpaceURL {
					return fmt.Errorf("%s in %s points at %q, want %s", SpaceServer, path, e.URL, SpaceURL)
				}
				if !enabled {
					return fmt.Errorf("%s is disabled in %s", SpaceServer, path)
				}
				found = true
				continue
			}
			if enabled && isOtherSpaceHost(e.URL) {
				return fmt.Errorf("MCP server %q in %s points at %q; only %s may reach Space", name, path, e.URL, SpaceURL)
			}
		}
	}
	if !found {
		return fmt.Errorf("%s is not configured in %s", SpaceServer, project)
	}
	return nil
}

func isOtherSpaceHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "space.birunidev.com" {
		return u.Path != "/mcp"
	}
	return h == "workspace.birunidev.com" || strings.HasSuffix(h, ".workspace.birunidev.com")
}
