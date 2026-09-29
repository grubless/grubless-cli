// Package client is the HTTP client. Everything the CLI knows about the
// server goes through here, including how each failure mode becomes a message
// a person can act on. A thin client: it never computes a tax figure.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/grubless/grubless-cli/internal/output"
)

const Version = "0.1.0"

// minVersionHeader is the server's floor for supported CLIs. It shipped in
// v1 so that it existed before anyone was pinned to an old version:
// retrofitting a handshake onto a fleet of deployed CLIs is the problem it
// avoids. A raised floor warns rather than fails — it must not break a firm's
// nightly job at 2am.
const minVersionHeader = "x-grubless-min-cli-version"

type Client struct {
	apiURL string
	token  string
	http   *http.Client

	// Once, not a bool: whoami issues requests concurrently, and two
	// goroutines racing on a plain flag could both print the warning.
	versionWarning sync.Once
}

func New(apiURL, token string) *Client {
	// No overall timeout, matching fetch: a report download over a slow link
	// can legitimately take minutes, and --wait has its own deadline.
	return &Client{apiURL: apiURL, token: token, http: &http.Client{}}
}

func (c *Client) checkVersion(res *http.Response) {
	min := res.Header.Get(minVersionHeader)
	if min == "" || CompareVersions(Version, min) >= 0 {
		return
	}
	c.versionWarning.Do(func() {
		fmt.Fprintf(os.Stderr,
			"warning: this CLI is %s, but the server requires %s or newer.\n"+
				"         Update from https://github.com/grubless/grubless-cli/releases\n", Version, min)
	})
}

// fail turns a non-2xx response into a CliError carrying the server's own
// message wherever one exists. A 402 shows the server's own message, which is
// written for a person; a 403 from a read-only token is told apart from a
// role 403 because the two have completely different fixes.
func fail(res *http.Response, method, path string) error {
	body, _ := io.ReadAll(res.Body)

	var parsed struct {
		Error         json.RawMessage `json:"error"`
		ReadOnlyToken bool            `json:"readOnlyToken"`
	}
	_ = json.Unmarshal(body, &parsed)

	var errString string
	isString := json.Unmarshal(parsed.Error, &errString) == nil
	// `typeof x === "object"` in the TS, which is true for arrays too.
	isObject := len(parsed.Error) > 0 && (parsed.Error[0] == '{' || parsed.Error[0] == '[')

	message := errString
	if !isString {
		message = truncateRunes(strings.TrimSpace(string(body)), 500)
		if message == "" {
			message = http.StatusText(res.StatusCode)
		}
	}

	switch {
	case res.StatusCode == 401:
		return output.Errorf(output.AuthFailure,
			"%s\nRun `grubless auth login` to authenticate, or set GRUBLESS_TOKEN.", message)
	case res.StatusCode == 403 && parsed.ReadOnlyToken:
		return output.Errorf(output.AuthFailure,
			"%s\nThis is a token-scope problem, not a permissions problem — create a write-scoped token.", message)
	case res.StatusCode == 403:
		return output.Errorf(output.Failure, "%s\nYour account's role on this entity doesn't allow that.", message)
	case res.StatusCode == 402:
		return output.Errorf(output.UpgradeRequired, "%s\nUpgrade at https://grubless.io/app", message)
	case res.StatusCode == 404:
		return output.Errorf(output.Failure, "%s", message)
	case isObject:
		// Zod's flattened issue shape — shown raw rather than prettified.
		return output.Errorf(output.UsageError, "%s %s rejected:\n%s", method, path, indent(parsed.Error))
	}
	return output.Errorf(output.Failure, "%s %s failed (%d): %s", method, path, res.StatusCode, message)
}

func (c *Client) send(method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	url := strings.TrimSuffix(c.apiURL, "/") + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, output.Errorf(output.Failure, "Could not reach %s: %v", c.apiURL, err)
	}
	req.Header.Set("user-agent", fmt.Sprintf("grubless-cli/%s (go %s)", Version, runtime.Version()))
	req.Header.Set("accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}

	res, err := c.http.Do(req)
	if err != nil {
		// Names the host: the most common cause is pointing at the wrong one.
		return nil, output.Errorf(output.Failure, "Could not reach %s: %v", c.apiURL, err)
	}
	c.checkVersion(res)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		defer res.Body.Close()
		return nil, fail(res, method, path)
	}
	return res, nil
}

// GetRaw returns the response body undecoded, for `--json` passthrough.
func (c *Client) GetRaw(path string) ([]byte, error) {
	res, err := c.send("GET", path, nil, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// Get decodes a JSON response into out. Unknown fields are ignored, like a TS
// cast — a server adding a field must never break an installed CLI.
func (c *Client) Get(path string, out any) error {
	raw, err := c.GetRaw(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return output.Errorf(output.Failure, "GET %s: unexpected response shape: %v", path, err)
	}
	return nil
}

// CompareVersions is a numeric-segment comparison; enough for x.y.z.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := segment(pa, i), segment(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// segment mimics `Number.parseInt(n, 10) || 0`: leading digits, else 0.
func segment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	s := parts[i]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// indent pretty-prints raw JSON in place. Not MarshalIndent via a map: that
// sorts keys, and Zod's field order is the order the form has them in.
func indent(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") != nil {
		return string(raw)
	}
	return buf.String()
}
