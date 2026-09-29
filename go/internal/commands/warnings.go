package commands

import (
	"fmt"
	"strconv"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// Which categories block a filing and which are advisory — a fixed split,
// so the CI gate means the same thing on every run in every firm. See
// src/commands/warnings.ts for what each one means for the figures.
var (
	blocking   = []string{"zero-cost", "uncategorized-transfers"}
	advisory   = []string{"unpriced-assets", "unbalanced-transfers"}
	categories = append(append([]string{}, blocking...), advisory...)
)

var categoryLabel = map[string]string{
	"zero-cost":               "Disposals with no cost basis",
	"uncategorized-transfers": "Uncategorised transfers",
	"unpriced-assets":         "Assets with no price",
	"unbalanced-transfers":    "Unbalanced transfers",
}

type categoryResult struct {
	category string
	blocking bool
	raw      []byte
	rows     []jsonv.Value
}

func Warnings(c *client.Client, s Scope, failOnBlocking bool) (int, error) {
	entities, err := ResolveScope(c, s)
	if err != nil {
		return 0, err
	}
	blockingTotal := 0
	var payload []jsonv.Value
	multi := len(entities) > 1

	for _, entity := range entities {
		var results []categoryResult
		for i, category := range categories {
			raw, err := c.GetRaw("/entities/" + entity.ID + "/warnings/" + category)
			if err != nil {
				return 0, err
			}
			value, err := api.Value(raw)
			if err != nil {
				return 0, err
			}
			results = append(results, categoryResult{category, i < len(blocking), raw, items(value)})
		}

		entityBlocking := 0
		for _, r := range results {
			if r.blocking {
				entityBlocking += len(r.rows)
			}
		}
		blockingTotal += entityBlocking

		if s.JSON {
			byCategory := jsonv.NewObject()
			for _, r := range results {
				byCategory.Set(r.category, r.rows)
			}
			payload = append(payload, jsonv.Obj("entity", entityRef(entity), "blockingCount", entityBlocking, "categories", byCategory))
			continue
		}

		if multi {
			output.Out(output.Bold(entity.Name))
		}
		if err := renderWarnings(results); err != nil {
			return 0, err
		}
		if entityBlocking == 0 {
			output.Note(output.Green("✓") + " No blocking issues.")
		} else {
			output.Note(output.Yellow("!") + fmt.Sprintf(" %d blocking issue(s) — review before filing.", entityBlocking))
		}
		if multi {
			output.Out("")
		}
	}

	if s.JSON {
		// Keyed on the count, unlike the other commands — kept as the TS has it.
		if len(entities) == 1 {
			output.JSON(payload[0])
		} else {
			output.JSON(payload)
		}
	}
	if failOnBlocking && blockingTotal > 0 {
		return output.BlockingWarnings, nil
	}
	return output.Ok, nil
}

func renderWarnings(results []categoryResult) error {
	output.Table(results, []output.Column[categoryResult]{
		{Header: "CATEGORY", Value: func(r categoryResult) string { return categoryLabel[r.category] }},
		{Header: "COUNT", Value: func(r categoryResult) string { return strconv.Itoa(len(r.rows)) }, AlignRight: true},
		{Header: "", Value: func(r categoryResult) string {
			switch {
			case len(r.rows) == 0:
				return ""
			case r.blocking:
				return output.Yellow("blocking")
			}
			return output.Dim("advisory")
		}},
	})

	// A few concrete examples per non-empty blocking category: a bare count
	// says there's a problem but doesn't help anyone start fixing it.
	for _, r := range results {
		if !r.blocking || len(r.rows) == 0 {
			continue
		}
		count := len(r.rows)
		output.Out("")
		output.Out(output.Dim(fmt.Sprintf("%s — first %d of %d:", categoryLabel[r.category], min(5, count), count)))
		if r.category == "zero-cost" {
			rows, err := api.Decode[[]api.ZeroCostWarning](r.raw)
			if err != nil {
				return err
			}
			output.Table(rows[:min(5, len(rows))], []output.Column[api.ZeroCostWarning]{
				{Header: "DATE", Value: func(w api.ZeroCostWarning) string { return jsstr.Slice(w.Ts, 0, 10) }},
				{Header: "ASSET", Value: func(w api.ZeroCostWarning) string { return w.AssetSymbol }},
				{Header: "QUANTITY", Value: func(w api.ZeroCostWarning) string { return output.Qty(w.Quantity) }, AlignRight: true},
				{Header: "PROCEEDS", Value: func(w api.ZeroCostWarning) string { return output.Money(w.ProceedsAmount) }, AlignRight: true},
				{Header: "SOURCE", Value: func(w api.ZeroCostWarning) string { return w.SourceLabel }},
			})
		} else {
			rows, err := api.Decode[[]api.UncategorizedTransferWarning](r.raw)
			if err != nil {
				return err
			}
			output.Table(rows[:min(5, len(rows))], []output.Column[api.UncategorizedTransferWarning]{
				{Header: "DATE", Value: func(w api.UncategorizedTransferWarning) string { return jsstr.Slice(w.Ts, 0, 10) }},
				{Header: "DIR", Value: func(w api.UncategorizedTransferWarning) string { return w.Direction }},
				{Header: "ASSET", Value: func(w api.UncategorizedTransferWarning) string { return w.AssetSymbol }},
				{Header: "AMOUNT", Value: func(w api.UncategorizedTransferWarning) string { return output.Qty(w.Amount) }, AlignRight: true},
			})
		}
	}
	return nil
}
