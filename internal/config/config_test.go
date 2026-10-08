package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func environment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestPlatformPaths(t *testing.T) {
	home := t.TempDir()
	for _, platform := range []string{"linux", "darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			p, err := PlatformPaths(platform, home, environment(map[string]string{"APPDATA": home, "LOCALAPPDATA": home}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(p.Config, "nemalo") || !filepath.IsAbs(p.State) || !filepath.IsAbs(p.Cache) {
				t.Fatalf("bad paths: %+v", p)
			}
		})
	}
	custom := filepath.Join(home, "custom")
	p, err := PlatformPaths("linux", home, environment(map[string]string{"XDG_CONFIG_HOME": custom, "XDG_STATE_HOME": custom, "XDG_CACHE_HOME": custom}))
	if err != nil || p.Config != filepath.Join(custom, "nemalo", "config.json") {
		t.Fatalf("XDG override: %+v %v", p, err)
	}
	for _, tc := range []struct {
		platform, home string
		values         map[string]string
	}{{"unsupported", home, nil}, {"linux", home, map[string]string{"XDG_STATE_HOME": "relative"}}, {"linux", "relative", nil}, {"windows", home, nil}} {
		if _, err := PlatformPaths(tc.platform, tc.home, environment(tc.values)); err == nil {
			t.Fatalf("accepted invalid paths: %+v", tc)
		}
	}
	if _, err := DefaultPaths(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	library := filepath.Join(dir, "books")
	review := filepath.Join(dir, "review")
	if err := os.WriteFile(path, []byte(`{"library":`+quote(library)+`,"review":`+quote(review)+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true, environment(nil), Config{})
	if err != nil || c.Library != library || c.Review != review {
		t.Fatalf("file: %+v %v", c, err)
	}
	envLibrary := filepath.Join(dir, "env")
	flagLibrary := filepath.Join(dir, "flag")
	flagReview := filepath.Join(dir, "flagreview")
	c, err = Load(path, true, environment(map[string]string{"NEMALO_LIBRARY": envLibrary, "NEMALO_REVIEW": review}), Config{Library: flagLibrary, Review: flagReview})
	if err != nil || c.Library != flagLibrary || c.Review != flagReview {
		t.Fatalf("flags: %+v %v", c, err)
	}
	c, err = Load(path, true, environment(map[string]string{"NEMALO_LIBRARY": envLibrary}), Config{})
	if err != nil || c.Library != envLibrary {
		t.Fatalf("environment: %+v %v", c, err)
	}
	if _, err = Load(filepath.Join(dir, "missing"), false, environment(nil), Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(filepath.Join(dir, "missing"), true, environment(nil), Config{}); err == nil {
		t.Fatal("explicit missing file accepted")
	}
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestRejectInvalidConfiguration(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	for _, body := range []string{"null", "[]", "{", "{} {}", `{"unknown":1}`, `{"library":1}`, strings.Repeat(" ", maxConfigBytes+1)} {
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(file, true, environment(nil), Config{}); err == nil {
			t.Fatalf("accepted invalid config %q", body[:min(len(body), 30)])
		}
	}
	if _, err := Load(dir, true, environment(nil), Config{}); err == nil {
		t.Fatal("directory accepted")
	}
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Config{{Library: "relative"}, {Review: "relative"}, {Library: dir, Review: dir}, {Library: dir, Review: filepath.Join(dir, "review")}, {Review: dir, Library: filepath.Join(dir, "books")}} {
		if _, err := Load(file, true, environment(nil), c); err == nil {
			t.Fatalf("accepted invalid directories: %+v", c)
		}
	}
}
