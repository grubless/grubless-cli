// Package txfmt turns transactions into the short strings a table cell or a
// detail pane shows. Shared by `grubless transactions` and the TUI's tab, so
// a transaction reads the same in both.
//
// Like every other figure in the CLI, amounts are formatted from the server's
// exact decimal strings and never pass through a float.
package txfmt

import (
	"strconv"
	"strings"

	"github.com/grubless/grubless-cli/internal/api"
	"github.com/grubless/grubless-cli/internal/jsstr"
	"github.com/grubless/grubless-cli/internal/output"
)

// Assets indexes a page's assets by id, for leg symbols.
type Assets map[string]api.TxAsset

func (a Assets) Add(assets []api.TxAsset) {
	for _, asset := range assets {
		a[asset.ID] = asset
	}
}

// Symbol is a leg's asset symbol, or a short id when the page didn't carry
// the asset — an unlabelled amount would be worse than an ugly one.
func (a Assets) Symbol(assetID string) string {
	if asset, ok := a[assetID]; ok && asset.Symbol != "" {
		return asset.Symbol
	}
	return ShortID(assetID)
}

// ShortID is the first 8 characters of a uuid — enough to tell rows apart
// on screen; --json carries the full id.
func ShortID(id string) string { return jsstr.Slice(id, 0, 8) }

// When is a timestamp as "2026-09-29 01:30", in UTC — which the column
// header says, because a date without its timezone is a guess about which
// financial year a midnight trade falls in.
func When(ts string) string {
	t, ok := output.ParseTime(ts)
	if !ok {
		return ts
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// Type is the event type, marked when a person categorised it by hand or it
// was matched as an internal transfer — both change how it's taxed.
func Type(e api.TxEvent) string {
	label := e.EventType
	if e.IsInternalTransfer {
		label += " (internal)"
	}
	if e.IsManuallyCategorized {
		label += "*"
	}
	return label
}

// Legs summarises the non-fee legs moving in one direction: "1,250.5 SOL",
// or the first plus "+N more" when a transaction moved several assets. The
// count is kept rather than dropped, for the same reason the holdings tile
// counts what it can't fit.
func Legs(e api.TxEvent, direction string, assets Assets) string {
	var parts []string
	for _, leg := range e.Legs {
		if leg.Direction == direction && leg.Role != "fee" {
			parts = append(parts, output.Qty(leg.Amount)+" "+assets.Symbol(leg.AssetID))
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return parts[0] + " +" + strconv.Itoa(len(parts)-1)
}

// Fee is the fee legs, as amount and symbol.
func Fee(e api.TxEvent, assets Assets) string {
	var parts []string
	for _, leg := range e.Legs {
		if leg.Role == "fee" {
			parts = append(parts, output.Qty(leg.Amount)+" "+assets.Symbol(leg.AssetID))
		}
	}
	return strings.Join(parts, ", ")
}

// GainLoss is the realised gain or loss across every leg, summed exactly, or
// nil when no leg realised anything (a receive, a transfer) — shown as "—",
// which is a different statement from "0.00".
func GainLoss(e api.TxEvent) *string {
	values := make([]*string, len(e.Legs))
	for i, leg := range e.Legs {
		values[i] = leg.GainLoss
	}
	return output.SumDecimals(values...)
}

// Tags joins the event's tag labels.
func Tags(e api.TxEvent) string {
	labels := make([]string, len(e.Tags))
	for i, t := range e.Tags {
		labels[i] = t.Label
	}
	return strings.Join(labels, ", ")
}

// Source is the event's source label, or "—".
func Source(e api.TxEvent) string {
	if e.Source == nil || e.Source.Label == "" {
		return "—"
	}
	return e.Source.Label
}
