// Package config decides where the API token lives, and in what order it's
// looked for: env, then keychain, then a 0600 file. The reasoning for that
// order is in src/config.ts and applies unchanged.
//
// The keychain is still driven by shelling out to the platform's own tool.
// In Go that's no longer forced — there's no Node ABI to build native addons
// against — but it keeps this package dependency-free, and linking the
// Security framework on macOS would need cgo, which would end trivial
// cross-compilation.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	service       = "grubless-cli"
	account       = "api-token"
	DefaultAPIURL = "https://api.grubless.io"
)

type Config struct {
	APIURL string
	Token  string
	// TokenSource is "env", "keychain", "file", or "" when there's no token.
	TokenSource string
}

func configDir() string {
	base, ok := os.LookupEnv("XDG_CONFIG_HOME")
	if !ok {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "grubless")
}

func Path() string { return filepath.Join(configDir(), "config.json") }

// stored mirrors the file's shape. Pointers because the file distinguishes
// "absent" from "empty", and `omitempty` on a pointer drops only the absent.
type stored struct {
	APIURL *string `json:"apiUrl,omitempty"`
	Token  *string `json:"token,omitempty"`
	// Theme is the TUI's, saved when it's switched with `t`. Go only; the
	// Node build ignores the field.
	Theme *string `json:"theme,omitempty"`
}

func readStored() stored {
	var s stored
	path := Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	// A token file readable by other accounts is worth saying out loud. Warn
	// rather than refuse — see src/config.ts.
	if info, err := os.Stat(path); err == nil && runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			fmt.Fprintf(os.Stderr, "warning: %s is readable by other users (mode %o). Run: chmod 600 %s\n", path, mode, path)
		}
	}
	if json.Unmarshal(raw, &s) != nil {
		return stored{}
	}
	return s
}

func writeStored(next stored) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	path := Path()
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	// WriteFile's mode only applies on create, same as Node's — an existing
	// world-readable file would stay that way without this.
	return os.Chmod(path, 0o600)
}

// ---------- keychain ----------

// run executes a keychain tool with stdin from `input` (or /dev/null) and
// stderr discarded, returning trimmed stdout.
func run(input string, name string, arg ...string) (string, error) {
	cmd := exec.Command(name, arg...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func keychainGet() string {
	var out string
	var err error
	switch runtime.GOOS {
	case "darwin":
		out, err = run("", "security", "find-generic-password", "-s", service, "-a", account, "-w")
	case "linux":
		out, err = run("", "secret-tool", "lookup", "service", service, "account", account)
	default:
		return ""
	}
	// Tool missing, or no entry stored: both mean "no keychain token here".
	if err != nil {
		return ""
	}
	return out
}

func keychainSet(token string) bool {
	var err error
	switch runtime.GOOS {
	case "darwin":
		_, err = run("", "security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", token)
	case "linux":
		_, err = run(token, "secret-tool", "store", "--label=Grubless CLI", "service", service, "account", account)
	default:
		return false
	}
	return err == nil
}

func keychainClear() {
	switch runtime.GOOS {
	case "darwin":
		_, _ = run("", "security", "delete-generic-password", "-s", service, "-a", account)
	case "linux":
		_, _ = run("", "secret-tool", "clear", "service", service, "account", account)
	}
}

// ---------- public ----------

// Load resolves the config. apiURL/hasAPIURL is the --api-url flag; the
// presence bool matters because `--api-url ""` is a given value in the TS
// (`??` only skips undefined), and so is an empty GRUBLESS_API_URL.
func Load(apiURL string, hasAPIURL bool) Config {
	s := readStored()

	url := DefaultAPIURL
	switch {
	case hasAPIURL:
		url = apiURL
	case envSet("GRUBLESS_API_URL"):
		url = os.Getenv("GRUBLESS_API_URL")
	case s.APIURL != nil:
		url = *s.APIURL
	}

	if env := strings.TrimSpace(os.Getenv("GRUBLESS_TOKEN")); env != "" {
		return Config{APIURL: url, Token: env, TokenSource: "env"}
	}

	if kc := keychainGet(); kc != "" {
		return Config{APIURL: url, Token: kc, TokenSource: "keychain"}
	}
	if s.Token != nil && *s.Token != "" {
		return Config{APIURL: url, Token: *s.Token, TokenSource: "file"}
	}
	return Config{APIURL: url}
}

func envSet(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

// SaveToken stores the token and returns where it ended up — "keychain" or
// "file" — so login can tell the user the truth.
func SaveToken(token, apiURL string) (string, error) {
	s := readStored()
	if keychainSet(token) {
		// Don't leave a copy behind in the file: two places to revoke is one
		// too many. Everything else in the file is kept.
		s.APIURL, s.Token = &apiURL, nil
		return "keychain", writeStored(s)
	}
	s.APIURL = &apiURL
	s.Token = &token
	return "file", writeStored(s)
}

func ClearToken() error {
	keychainClear()
	s := readStored()
	if s.Token != nil && *s.Token != "" {
		s.Token = nil
		return writeStored(s)
	}
	return nil
}

// Theme is the TUI theme: GRUBLESS_THEME, then the saved one, then "".
// fromEnv says which, so the TUI can tell someone why switching with `t`
// won't outlast the session.
func Theme() (theme string, fromEnv bool) {
	if env := strings.TrimSpace(os.Getenv("GRUBLESS_THEME")); env != "" {
		return env, true
	}
	if s := readStored(); s.Theme != nil {
		return *s.Theme, false
	}
	return "", false
}

// SaveTheme records the TUI theme, keeping everything else in the file.
func SaveTheme(theme string) error {
	s := readStored()
	s.Theme = &theme
	return writeStored(s)
}
