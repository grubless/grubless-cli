package commands

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/config"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// There is deliberately no password login: see src/commands/auth.ts.

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

	// Verify before storing: a bad token found only on the next command is a
	// needlessly confusing failure.
	entities, _, err := fetch[[]jsonv.Value](client.New(cfg.APIURL, token), "/entities")
	if err != nil {
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

// AuthWhoami fetches entities and tokens concurrently, as the TS's
// Promise.all does.
//
// Note: like the TS, this reports the API URL from config and environment,
// ignoring --api-url, even though the requests themselves honour it. That's
// a bug in both builds, kept here so they agree until it's fixed in both.
func AuthWhoami(c *client.Client, asJSON bool) (int, error) {
	cfg := config.Load("", false)

	type result struct {
		entities []api.Entity
		value    jsonv.Value
		err      error
	}
	entitiesCh := make(chan result, 1)
	tokensCh := make(chan result, 1)
	go func() {
		e, v, err := fetch[[]api.Entity](c, "/entities")
		entitiesCh <- result{e, v, err}
	}()
	go func() {
		_, v, err := fetch[[]jsonv.Value](c, "/api-tokens")
		tokensCh <- result{nil, v, err}
	}()

	ents := <-entitiesCh
	if ents.err != nil {
		return 0, ents.err
	}
	toks := <-tokensCh
	// A read-scoped token can list tokens, but a failure here isn't worth
	// failing whoami over.
	if toks.err != nil {
		toks.value = []jsonv.Value{}
	}

	if asJSON {
		var source jsonv.Value
		if cfg.TokenSource != "" {
			source = cfg.TokenSource
		}
		output.JSON(jsonv.Obj("apiUrl", cfg.APIURL, "tokenSource", source, "entities", ents.value, "tokens", toks.value))
		return output.Ok, nil
	}

	source := cfg.TokenSource
	if source == "" {
		source = "nowhere — not signed in"
	}
	output.Note("API      " + cfg.APIURL)
	output.Note("Token    from " + source)
	output.Note("")
	if len(ents.entities) == 0 {
		output.Note("No entities available to this account.")
		return output.Ok, nil
	}
	output.Table(ents.entities, []output.Column[api.Entity]{
		{Header: "ENTITY", Value: func(e api.Entity) string { return e.Name }},
		{Header: "TYPE", Value: func(e api.Entity) string { return e.EntityType }},
		{Header: "ROLE", Value: func(e api.Entity) string { return e.Role }},
		{Header: "ID", Value: func(e api.Entity) string { return e.ID }},
	})
	return output.Ok, nil
}
