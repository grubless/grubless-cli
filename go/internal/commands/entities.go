// Package commands holds the scriptable subcommands, one file per group, each
// a port of the matching file in src/commands/.
package commands

import (
	"fmt"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// fetch GETs a path and returns both views of it: the typed one for tables,
// and the ordered value for `--json`.
func fetch[T any](c *client.Client, path string) (T, jsonv.Value, error) {
	var zero T
	raw, err := c.GetRaw(path)
	if err != nil {
		return zero, nil, err
	}
	typed, err := api.Decode[T](raw)
	if err != nil {
		return zero, nil, fmt.Errorf("GET %s: %w", path, err)
	}
	value, err := api.Value(raw)
	if err != nil {
		return zero, nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return typed, value, nil
}

// items is a JSON array's elements, or none if the value isn't an array.
func items(v jsonv.Value) []jsonv.Value {
	arr, _ := v.([]jsonv.Value)
	return arr
}

// entityRef is the `{ id, name }` every multi-entity output is keyed by.
func entityRef(e api.Entity) *jsonv.Object {
	return jsonv.Obj("id", e.ID, "name", e.Name)
}

func EntitiesList(c *client.Client, asJSON bool) (int, error) {
	entities, value, err := fetch[[]api.Entity](c, "/entities")
	if err != nil {
		return 0, err
	}
	if asJSON {
		output.JSON(value)
		return output.Ok, nil
	}
	if len(entities) == 0 {
		// stderr: an empty list piped into jq should be empty, not prose.
		output.Note("No entities.")
		return output.Ok, nil
	}
	output.Table(entities, []output.Column[api.Entity]{
		{Header: "ID", Value: func(e api.Entity) string { return e.ID }},
		{Header: "NAME", Value: func(e api.Entity) string { return e.Name }},
		{Header: "TYPE", Value: func(e api.Entity) string { return e.EntityType }},
		{Header: "ROLE", Value: func(e api.Entity) string { return e.Role }},
	})
	return output.Ok, nil
}

// ResolveEntity turns a uuid, an exact name, or an unambiguous
// case-insensitive prefix into an entity. Ambiguity is an error, never a
// guess: the wrong client's entity would produce a confident, correct-looking,
// completely wrong tax report.
func ResolveEntity(c *client.Client, needle string) (api.Entity, error) {
	entities, _, err := fetch[[]api.Entity](c, "/entities")
	if err != nil {
		return api.Entity{}, err
	}
	for _, e := range entities {
		if e.ID == needle {
			return e, nil
		}
	}

	// toLowerCase in the TS; strings.ToLower agrees except on a few special
	// casings (a final sigma, dotted İ) that no entity name is likely to hit.
	lower := strings.ToLower(needle)
	var exact, prefix []api.Entity
	for _, e := range entities {
		name := strings.ToLower(e.Name)
		if name == lower {
			exact = append(exact, e)
		}
		if strings.HasPrefix(name, lower) {
			prefix = append(prefix, e)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return api.Entity{}, ambiguous(needle, exact)
	case len(prefix) == 1:
		return prefix[0], nil
	case len(prefix) > 1:
		return api.Entity{}, ambiguous(needle, prefix)
	}
	return api.Entity{}, output.Errorf(output.UsageError,
		"No entity matching \"%s\".\nRun `grubless entities list` to see what this account can reach.", needle)
}

func ambiguous(needle string, matches []api.Entity) error {
	lines := make([]string, len(matches))
	for i, e := range matches {
		lines[i] = fmt.Sprintf("  %s  %s", e.ID, e.Name)
	}
	return output.Errorf(output.UsageError, "\"%s\" matches %d entities:\n%s\nUse the id.", needle, len(matches), strings.Join(lines, "\n"))
}

// Scope is the entity selection shared by most commands.
type Scope struct {
	Entity      string
	AllEntities bool
	JSON        bool
}

// ResolveScope is one resolved entity, or every entity for --all-entities.
func ResolveScope(c *client.Client, s Scope) ([]api.Entity, error) {
	if s.AllEntities {
		entities, _, err := fetch[[]api.Entity](c, "/entities")
		if err != nil {
			return nil, err
		}
		if len(entities) == 0 {
			return nil, output.Errorf(output.Failure, "This account has no entities.")
		}
		return entities, nil
	}
	if s.Entity == "" {
		return nil, output.Errorf(output.UsageError, "Specify --entity <id|name>, or --all-entities.")
	}
	e, err := ResolveEntity(c, s.Entity)
	if err != nil {
		return nil, err
	}
	return []api.Entity{e}, nil
}
