package commands

import (
	"fmt"
	"os"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// sourceRow is a source with the entity it belongs to, in both views.
type sourceRow struct {
	api.Source
	entityName string
}

func SourcesList(c *client.Client, s Scope) (int, error) {
	entities, err := ResolveScope(c, s)
	if err != nil {
		return 0, err
	}
	var rows []sourceRow
	values := []jsonv.Value{}
	for _, entity := range entities {
		sources, value, err := fetch[[]api.Source](c, "/entities/"+entity.ID+"/sources")
		if err != nil {
			return 0, err
		}
		for i, src := range sources {
			rows = append(rows, sourceRow{src, entity.Name})
			// `{ ...s, entityName }`: every server field, then the name.
			obj := jsonv.NewObject()
			if o, ok := items(value)[i].(*jsonv.Object); ok {
				for _, k := range o.Keys() {
					v, _ := o.Get(k)
					obj.Set(k, v)
				}
			}
			obj.Set("entityName", entity.Name)
			values = append(values, obj)
		}
	}

	if s.JSON {
		output.JSON(values)
		return output.Ok, nil
	}
	if len(rows) == 0 {
		output.Note("No sources.")
		return output.Ok, nil
	}

	columns := []output.Column[sourceRow]{{Header: "ID", Value: func(r sourceRow) string { return r.ID }}}
	if len(entities) > 1 {
		columns = append(columns, output.Column[sourceRow]{Header: "ENTITY", Value: func(r sourceRow) string { return r.entityName }})
	}
	columns = append(columns,
		output.Column[sourceRow]{Header: "LABEL", Value: func(r sourceRow) string { return r.Label }},
		output.Column[sourceRow]{Header: "ADAPTER", Value: func(r sourceRow) string { return r.AdapterKey }},
		output.Column[sourceRow]{Header: "TXNS", Value: func(r sourceRow) string { return countString(r.TransactionCount) }, AlignRight: true},
		output.Column[sourceRow]{Header: "SYNCED", Value: func(r sourceRow) string { return output.ShortDate(r.LastSyncedAt) }},
		output.Column[sourceRow]{Header: "STATUS", Value: func(r sourceRow) string {
			// A disabled source reads "off", not "idle": idle implies it will
			// sync on the next run, and this one never will until re-enabled.
			switch {
			case !r.SyncEnabled:
				return "off"
			case r.SyncStatus == "error":
				return output.Red("error")
			}
			return r.SyncStatus
		}},
	)
	output.Table(rows, columns)

	for _, r := range rows {
		if r.SyncStatus == "error" {
			reason := "sync failed"
			if r.SyncError != nil {
				reason = *r.SyncError
			}
			output.Note(output.Red(fmt.Sprintf("✗ %s: %s", r.Label, reason)))
		}
	}
	return output.Ok, nil
}

// countString is `String(n)`, including "undefined" for an absent count.
func countString(n *float64) string {
	if n == nil {
		return "undefined"
	}
	return jsstr.Number(*n)
}

type SyncOptions struct {
	Scope
	Source    string
	All       bool
	Full      bool
	Wait      bool
	TimeoutMs float64
}

func SourcesSync(c *client.Client, o SyncOptions) (int, error) {
	if o.Source == "" && !o.All {
		return 0, output.Errorf(output.UsageError, "Specify --source <id>, or --all to sync every source on the entity.")
	}
	entities, err := ResolveScope(c, o.Scope)
	if err != nil {
		return 0, err
	}

	var results []jsonv.Value
	worst := output.Ok

	for _, entity := range entities {
		sources, _, err := fetch[[]api.Source](c, "/entities/"+entity.ID+"/sources")
		if err != nil {
			return 0, err
		}
		var targets []api.Source
		for _, src := range sources {
			if (o.All && src.SyncEnabled) || (!o.All && (src.ID == o.Source || src.Label == o.Source)) {
				targets = append(targets, src)
			}
		}

		if len(targets) == 0 {
			if o.All {
				output.Note(entity.Name + ": no syncable sources.")
				// Recorded, not omitted: "nothing to sync" must be
				// distinguishable from "not in the run".
				results = append(results, jsonv.Obj("entity", entityRef(entity), "queued", []jsonv.Value{}, "full", o.Full, "status", "skipped"))
				continue
			}
			return 0, output.Errorf(output.UsageError, "No source matching \"%s\" on %s.", o.Source, entity.Name)
		}

		// Captured BEFORE triggering. See waitForActivity.
		since := nowMs()

		for _, src := range targets {
			// skipPriceBackfill when firing several at once, so each sync
			// doesn't run its own entity-wide backfill against the same
			// rate-limited providers.
			if err := c.Post("/sources/"+src.ID+"/sync", map[string]bool{"full": o.Full, "skipPriceBackfill": len(targets) > 1}); err != nil {
				return 0, err
			}
			suffix := ""
			if o.Full {
				suffix = " (full resync)"
			}
			output.Note(output.Dim("→") + " queued " + src.Label + suffix)
		}

		queued := make([]jsonv.Value, len(targets))
		for i, src := range targets {
			queued[i] = jsonv.Obj("id", src.ID, "label", src.Label)
		}
		record := jsonv.Obj("entity", entityRef(entity), "queued", queued, "full", o.Full, "status", "queued")
		results = append(results, record)

		if !o.Wait {
			continue
		}
		// Under --json the live progress lines are noise: a caller piping
		// this wants the terminal state, not a replay of it.
		result, err := waitForActivity(c, entity.ID, since, o.TimeoutMs, o.JSON)
		if err != nil {
			return 0, err
		}
		exit := reportWaitResult(result, entity.Name+": sync finished")
		if exit == output.Ok {
			record.Set("status", "finished")
		} else {
			record.Set("status", "failed")
			worst = exit
		}
		record.Set("activity", result.values())
	}

	if !o.Wait {
		output.Note(output.Dim("Queued. Pass --wait to block until they finish."))
	}
	// Flag-keyed: --all-entities is always an array.
	if o.JSON {
		output.JSON(pick(o.AllEntities, results))
	}
	return worst, nil
}

// pick is `allEntities ? all : (all[0] ?? null)` — the shape follows the
// flag, not how many entities the account happens to hold today.
func pick(allEntities bool, all []jsonv.Value) jsonv.Value {
	if allEntities {
		if all == nil {
			return []jsonv.Value{}
		}
		return all
	}
	if len(all) == 0 {
		return nil
	}
	return all[0]
}

type ImportOptions struct {
	Entity    string
	Source    string
	Wait      bool
	TimeoutMs float64
	JSON      bool
}

// importLimit matches csvImportSchema's ceiling server-side, in UTF-16
// units as the TS measures it, so an oversized upload fails before the
// transfer rather than after.
const importLimit = 20 * 1024 * 1024

func SourcesImport(c *client.Client, filePath string, o ImportOptions) (int, error) {
	if o.Source == "" {
		return 0, output.Errorf(output.UsageError, "Specify --source <id> to import into.")
	}
	if o.Entity == "" {
		return 0, output.Errorf(output.UsageError, "Specify --entity <id|name>.")
	}
	entity, err := ResolveEntity(c, o.Entity)
	if err != nil {
		return 0, err
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		// Node reads by descriptor after opening, so a directory fails at
		// "read" (and names no path) where a missing file fails at "open".
		syscallName, path := "open", filePath
		if info, statErr := os.Stat(filePath); statErr == nil && info.IsDir() {
			syscallName, path = "read", ""
		}
		return 0, output.Errorf(output.UsageError, "Could not read %s: %s", filePath, output.NodeFSError(err, syscallName, path))
	}
	// readFileSync(p, "utf8") keeps a BOM, unlike fetch's text().
	fileContent := jsstr.DecodeUTF8(raw, false)
	length := jsstr.Len(fileContent)
	if length > importLimit {
		return 0, output.Errorf(output.UsageError, "%s is larger than the 20MB import limit.", filePath)
	}

	since := nowMs()
	if err := c.Post("/sources/"+o.Source+"/csv-import", map[string]string{"fileContent": fileContent}); err != nil {
		return 0, err
	}
	output.Note(fmt.Sprintf("%s uploaded %s (%sKB)", output.Dim("→"), filePath, jsstr.Number(jsstr.Round(float64(length)/1024))))

	record := jsonv.Obj(
		"entity", entityRef(entity),
		"source", o.Source,
		"file", filePath,
		"bytes", length,
		"status", "queued",
		"activity", jsonv.Undefined,
	)

	if !o.Wait {
		output.Note(output.Dim("Queued. Pass --wait to block until the import finishes."))
		if o.JSON {
			output.JSON(record)
		}
		return output.Ok, nil
	}

	result, err := waitForActivity(c, entity.ID, since, o.TimeoutMs, o.JSON)
	if err != nil {
		return 0, err
	}
	exit := reportWaitResult(result, "Import finished")
	if exit == output.Ok {
		record.Set("status", "finished")
	} else {
		record.Set("status", "failed")
	}
	record.Set("activity", result.values())
	if o.JSON {
		output.JSON(record)
	}
	return exit, nil
}

func Holdings(c *client.Client, s Scope) (int, error) {
	entities, err := ResolveScope(c, s)
	if err != nil {
		return 0, err
	}
	multi := len(entities) > 1

	for _, entity := range entities {
		rows, value, err := fetch[[]api.Holding](c, "/entities/"+entity.ID+"/holdings")
		if err != nil {
			return 0, err
		}
		if s.JSON {
			// One document per entity, as the TS prints them — unfiltered,
			// spam included.
			output.JSON(jsonv.Obj("entity", entityRef(entity), "holdings", value))
			continue
		}
		if multi {
			output.Out(output.Bold(entity.Name))
		}
		// Spam hidden, as in the web app: 200 airdropped phishing tokens
		// shouldn't bury the real position.
		var visible []api.Holding
		for _, h := range rows {
			if !h.IsSpam {
				visible = append(visible, h)
			}
		}
		output.Table(visible, []output.Column[api.Holding]{
			{Header: "ASSET", Value: func(h api.Holding) string { return h.Symbol }, MaxWidth: 20},
			{Header: "HELD IN", Value: func(h api.Holding) string { return output.HeldIn(h.Chain, h.SourceLabels(), 28) }},
			{Header: "QUANTITY", Value: func(h api.Holding) string { return output.Qty(h.Quantity) }, AlignRight: true},
			{Header: "VALUE", Value: func(h api.Holding) string { return output.Money(h.Value) }, AlignRight: true},
			// The reconciliation signal — the product's differentiator — does
			// not belong hidden behind --json.
			{Header: "", Value: func(h api.Holding) string {
				if h.HasMismatch {
					return output.Yellow("mismatch")
				}
				return ""
			}},
		})
		mismatched := 0
		for _, h := range visible {
			if h.HasMismatch {
				mismatched++
			}
		}
		if mismatched > 0 {
			output.Note(output.Yellow(fmt.Sprintf("! %d asset(s) disagree with the source's reported balance.", mismatched)))
		}
		if multi {
			output.Out("")
		}
	}
	return output.Ok, nil
}

// TimeoutMs converts --timeout minutes, or the default.
func TimeoutMs(minutes float64) float64 {
	if minutes > 0 {
		return minutes * 60_000
	}
	return DefaultWaitTimeoutMs
}
