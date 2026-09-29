package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hermetic points the config at a temp dir and hides every keychain tool, so
// tokens land in the file and nothing touches the developer's own.
func hermetic(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PATH", "")
	t.Setenv("GRUBLESS_TOKEN", "")
	t.Setenv("GRUBLESS_THEME", "")
	return filepath.Join(dir, "grubless", "config.json")
}

func TestThemeSurvivesTokenChanges(t *testing.T) {
	path := hermetic(t)
	if err := SaveTheme("cypher"); err != nil {
		t.Fatal(err)
	}
	// Signing in and out rewrites the file; the theme is someone else's
	// setting and must come through both.
	if where, err := SaveToken("grb_abc", "https://api.example"); err != nil || where != "file" {
		t.Fatalf("SaveToken: %s %v", where, err)
	}
	if theme, _ := Theme(); theme != "cypher" {
		t.Errorf("after login, theme = %q", theme)
	}
	if err := ClearToken(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if theme, _ := Theme(); theme != "cypher" || strings.Contains(string(raw), "grb_abc") {
		t.Errorf("after logout, theme %q, file %s", theme, raw)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o", info.Mode().Perm())
	}
}

func TestThemeEnvWins(t *testing.T) {
	hermetic(t)
	if err := SaveTheme("terminal"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRUBLESS_THEME", "cypher")
	if theme, fromEnv := Theme(); theme != "cypher" || !fromEnv {
		t.Errorf("Theme() = %q, %v", theme, fromEnv)
	}
}
