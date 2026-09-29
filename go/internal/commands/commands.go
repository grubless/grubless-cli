// Package commands holds the ported subcommands. In this spike: `auth` and
// `entities list`.
package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/config"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// Entity is this client's declared view of the wire shape (src/api-types.ts).
// Only what the table needs — `--json` passes the server's bytes through.
type Entity struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
	Role       string `json:"role"`
	CreatedAt  string `json:"createdAt"`
}

func EntitiesList(c *client.Client, asJSON bool) (int, error) {
	raw, err := c.GetRaw("/entities")
	if err != nil {
		return 0, err
	}
	if asJSON {
		return output.Ok, output.RawJSON(raw)
	}
	var entities []Entity
	if err := json.Unmarshal(raw, &entities); err != nil {
		return 0, output.Errorf(output.Failure, "GET /entities: unexpected response shape: %v", err)
	}
	if len(entities) == 0 {
		// stderr: an empty list piped into jq should be empty, not prose.
		output.Note("No entities.")
		return output.Ok, nil
	}
	output.Table(entities, []output.Column[Entity]{
		{Header: "ID", Value: func(e Entity) string { return e.ID }},
		{Header: "NAME", Value: func(e Entity) string { return e.Name }},
		{Header: "TYPE", Value: func(e Entity) string { return e.EntityType }},
		{Header: "ROLE", Value: func(e Entity) string { return e.Role }},
	})
	return output.Ok, nil
}

const tokenPage = "/app/settings/api-tokens"

var apiSubdomain = regexp.MustCompile(`^https://api\.`)

func AuthLogin(token string, apiURL string, hasAPIURL bool) (int, error) {
	cfg := config.Load(apiURL, hasAPIURL)

	token = strings.TrimSpace(token)
	if token == "" {
		if !output.IsTerminal(os.Stdin) {
			return 0, output.Errorf(output.UsageError,
				"No token given and stdin isn't a terminal.\n"+
					"In a script, pass --token, or set GRUBLESS_TOKEN instead of logging in.")
		}
		output.Note(fmt.Sprintf("Create a token at %s%s", apiSubdomain.ReplaceAllString(cfg.APIURL, "https://"), tokenPage))
		output.Note("")
		fmt.Fprint(os.Stderr, "Paste your API token: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		token = strings.TrimSpace(line)
	}
	if token == "" {
		return 0, output.Errorf(output.UsageError, "No token entered.")
	}

	// Verify before storing — see src/commands/auth.ts.
	var entities []json.RawMessage
	if err := client.New(cfg.APIURL, token).Get("/entities", &entities); err != nil {
		return 0, err
	}

	where, err := config.SaveToken(token, cfg.APIURL)
	if err != nil {
		return 0, output.Errorf(output.Failure, "Could not save the token: %v", err)
	}
	noun := "entities"
	if len(entities) == 1 {
		noun = "entity"
	}
	place := "system keychain"
	if where == "file" {
		place = fmt.Sprintf("config file (%s)", config.Path())
	}
	output.Note(output.Green("✓") + fmt.Sprintf(" Signed in — %d %s available.\n  Token stored in the %s.", len(entities), noun, place))
	return output.Ok, nil
}

func AuthLogout() (int, error) {
	if err := config.ClearToken(); err != nil {
		return 0, output.Errorf(output.Failure, "Could not update %s: %v", config.Path(), err)
	}
	output.Note(output.Green("✓") + " Signed out locally.\n" +
		"  The token still exists server-side — revoke it at " + tokenPage + " if it may have been exposed.")
	return output.Ok, nil
}

// AuthWhoami fetches entities and tokens concurrently — the Go shape of the
// TS's Promise.all.
func AuthWhoami(c *client.Client, asJSON bool) (int, error) {
	cfg := config.Load("", false)

	type result struct {
		raw []byte
		err error
	}
	entitiesCh := make(chan result, 1)
	tokensCh := make(chan result, 1)
	go func() { raw, err := c.GetRaw("/entities"); entitiesCh <- result{raw, err} }()
	go func() { raw, err := c.GetRaw("/api-tokens"); tokensCh <- result{raw, err} }()

	ents := <-entitiesCh
	if ents.err != nil {
		return 0, ents.err
	}
	toks := <-tokensCh
	// A failed token listing is not worth failing whoami over.
	if toks.err != nil {
		toks.raw = []byte("[]")
	}

	if asJSON {
		var source *string
		if cfg.TokenSource != "" {
			source = &cfg.TokenSource
		}
		return output.Ok, output.JSON(struct {
			APIURL      string          `json:"apiUrl"`
			TokenSource *string         `json:"tokenSource"`
			Entities    json.RawMessage `json:"entities"`
			Tokens      json.RawMessage `json:"tokens"`
		}{cfg.APIURL, source, ents.raw, toks.raw})
	}

	var entities []Entity
	if err := json.Unmarshal(ents.raw, &entities); err != nil {
		return 0, output.Errorf(output.Failure, "GET /entities: unexpected response shape: %v", err)
	}
	source := cfg.TokenSource
	if source == "" {
		source = "nowhere — not signed in"
	}
	output.Note("API      " + cfg.APIURL)
	output.Note("Token    from " + source)
	output.Note("")
	if len(entities) == 0 {
		output.Note("No entities available to this account.")
		return output.Ok, nil
	}
	output.Table(entities, []output.Column[Entity]{
		{Header: "ENTITY", Value: func(e Entity) string { return e.Name }},
		{Header: "TYPE", Value: func(e Entity) string { return e.EntityType }},
		{Header: "ROLE", Value: func(e Entity) string { return e.Role }},
		{Header: "ID", Value: func(e Entity) string { return e.ID }},
	})
	return output.Ok, nil
}
