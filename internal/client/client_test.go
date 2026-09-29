package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grubless/grubless-cli/internal/output"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "99.0.0", -1},
		{"1.10.0", "1.9.0", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.0-beta", "1.0.0", 0}, // parseInt("0-beta") is 0, same as the TS
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Each failure status maps to the exit code README § Exit codes promises, and
// keeps the server's own message.
func TestFailureMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantCode int
		wantText string
	}{
		{"rejected token", 401, `{"error":"Invalid token"}`, output.AuthFailure, "auth login"},
		{"read-only token", 403, `{"error":"Read-only token","readOnlyToken":true}`, output.AuthFailure, "token-scope"},
		{"role", 403, `{"error":"Forbidden"}`, output.Failure, "role on this entity"},
		{"billing", 402, `{"error":"Reports need a paid plan"}`, output.UpgradeRequired, "Reports need a paid plan"},
		{"not found", 404, `{"error":"No such entity"}`, output.Failure, "No such entity"},
		{"validation", 400, `{"error":{"fieldErrors":{"year":["Required"]}}}`, output.UsageError, `"fieldErrors"`},
		{"proxy html", 502, `<html>Bad Gateway</html>`, output.Failure, "failed (502): <html>Bad Gateway</html>"},
		{"empty body", 500, ``, output.Failure, "Internal Server Error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			var out any
			err := New(srv.URL, "grb_test").Get("/entities", &out)
			var cliErr *output.CliError
			if !errors.As(err, &cliErr) {
				t.Fatalf("got %v, want a CliError", err)
			}
			if cliErr.ExitCode != c.wantCode {
				t.Errorf("exit code = %d, want %d", cliErr.ExitCode, c.wantCode)
			}
			if !strings.Contains(cliErr.Message, c.wantText) {
				t.Errorf("message %q lacks %q", cliErr.Message, c.wantText)
			}
		})
	}
}

func TestSendsBearerAndNamesUnreachableHost(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	var out []any
	if err := New(srv.URL+"/", "grb_abc").Get("/entities", &out); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer grb_abc" {
		t.Errorf("authorization = %q", auth)
	}

	url := srv.URL
	srv.Close()
	err := New(url, "grb_abc").Get("/entities", &out)
	if err == nil || !strings.Contains(err.Error(), "Could not reach "+url) {
		t.Errorf("got %v, want the unreachable host named", err)
	}
}
