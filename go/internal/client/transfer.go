package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"unicode/utf8"

	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// Post sends a JSON body. The response is read and discarded: no caller in
// the CLI uses it, and the TS only parses it to return it.
func (c *Client) Post(path string, body any) error {
	var reader io.Reader
	headers := map[string]string{}
	if body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return err
		}
		reader = &buf
		headers["content-type"] = "application/json"
	}
	res, err := c.send("POST", path, reader, headers)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

// GetText is a text body — a report under `--json`, which must be whole
// before it can be re-framed, so there's nothing to stream.
func (c *Client) GetText(path string) (string, error) {
	res, err := c.send("GET", path, nil, map[string]string{"accept": "text/csv, */*"})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return jsstr.DecodeUTF8(raw, true), nil
}

// Download is a report body as a stream, so `--out` goes straight to disk.
// The caller closes Body.
type Download struct {
	Body io.ReadCloser
	// Filename is the server's Content-Disposition name, or "" for none.
	Filename string
}

func (c *Client) GetStream(path string) (*Download, error) {
	res, err := c.send("GET", path, nil, map[string]string{"accept": "*/*"})
	if err != nil {
		return nil, err
	}
	name, err := filenameFromDisposition(res.Header.Get("content-disposition"))
	if err != nil {
		res.Body.Close()
		return nil, err
	}
	return &Download{Body: res.Body, Filename: name}, nil
}

var dispositionFilename = regexp.MustCompile(`(?i)filename\*?=(?:UTF-8'')?"?([^";]+)"?`)

// filenameFromDisposition honours the server's suggested filename rather
// than inventing one.
func filenameFromDisposition(header string) (string, error) {
	if header == "" {
		return "", nil
	}
	m := dispositionFilename.FindStringSubmatch(header)
	if m == nil {
		return "", nil
	}
	return decodeURIComponent(m[1])
}

// decodeURIComponent, including its failure: a malformed escape throws
// URIError("URI malformed") in JS, which the report loop reports per entity.
func decodeURIComponent(s string) (string, error) {
	decoded, err := url.PathUnescape(s)
	if err != nil || (!utf8.ValidString(decoded) && utf8.ValidString(s)) {
		return "", &output.CliError{Message: "URI malformed", ExitCode: output.Failure}
	}
	return decoded, nil
}
