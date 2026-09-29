package commands

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/output"
	"github.com/grubless/grubless-cli/go/internal/txfmt"
)

// `grubless transactions` — an entity's transactions, filtered, newest first.
//
// A thin view over GET /entities/:id/tx-events, like everything else here:
// the filtering is the server's, and every figure is the server's decimal
// string. What this adds is paging (the API caps a page at 2000 and hands
// back a cursor) and name resolution, so `--source Kraken` and `--asset SOL`
// work where the API wants uuids.

const (
	defaultTxLimit = 50
	// The API's own ceiling per request.
	maxTxPage = 2000
)

type TxFilters struct {
	Category  string
	Direction string
	From      string
	To        string
	Search    string
	Source    string
	Asset     string
	Sort      string
	// Limit is "", a positive count, or "all".
	Limit string
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// parseLimit is the row cap, or -1 for all of them.
func parseLimit(s string) (int, error) {
	switch s {
	case "":
		return defaultTxLimit, nil
	case "all":
		return -1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, output.Errorf(output.UsageError, "--limit must be a positive number of transactions, or \"all\"; got \"%s\".", s)
	}
	return n, nil
}

func Transactions(c *client.Client, s Scope, f TxFilters) (int, error) {
	// Checked here, before any request, because they're typos rather than
	// questions for the server. Categories, dates and the rest the API
	// validates itself, and its errors name the field (exit 2).
	if f.Direction != "" && f.Direction != "in" && f.Direction != "out" {
		return 0, output.Errorf(output.UsageError, "--direction must be \"in\" or \"out\", got \"%s\".", f.Direction)
	}
	if f.Sort != "" && f.Sort != "asc" && f.Sort != "desc" {
		return 0, output.Errorf(output.UsageError, "--sort must be \"asc\" or \"desc\", got \"%s\".", f.Sort)
	}
	limit, err := parseLimit(f.Limit)
	if err != nil {
		return 0, err
	}

	entities, err := ResolveScope(c, s)
	if err != nil {
		return 0, err
	}

	var documents []jsonv.Value
	for i, entity := range entities {
		page, err := fetchTransactions(c, entity, f, limit)
		if err != nil {
			return 0, err
		}
		if s.JSON {
			documents = append(documents, page.document(entity, f))
			continue
		}
		if len(entities) > 1 {
			if i > 0 {
				output.Out("")
			}
			output.Out(output.Bold(entity.Name))
		}
		page.print(f)
	}

	// Flag-keyed, like every other command: --all-entities is always an array.
	if s.JSON {
		output.JSON(pick(s.AllEntities, documents))
	}
	return output.Ok, nil
}

// txResult is every page fetched for one entity, merged, in both views.
type txResult struct {
	events      []api.TxEvent
	eventValues []jsonv.Value
	assets      txfmt.Assets
	assetValues []jsonv.Value
	nextCursor  *string
}

func fetchTransactions(c *client.Client, entity api.Entity, f TxFilters, limit int) (*txResult, error) {
	query := url.Values{}
	set := func(key, value string) {
		if value != "" {
			query.Set(key, value)
		}
	}
	set("category", f.Category)
	set("direction", f.Direction)
	set("from", f.From)
	set("to", f.To)
	set("q", f.Search)
	set("sort", f.Sort)
	if f.Source != "" {
		id, err := resolveSourceID(c, entity, f.Source)
		if err != nil {
			return nil, err
		}
		query.Set("sourceId", id)
	}
	if f.Asset != "" {
		id, err := resolveAssetID(c, entity, f.Asset)
		if err != nil {
			return nil, err
		}
		query.Set("assetId", id)
	}

	r := &txResult{assets: txfmt.Assets{}}
	seenAssets := map[string]bool{}
	cursor := ""
	for {
		size := maxTxPage
		if limit >= 0 {
			size = min(maxTxPage, limit-len(r.events))
		}
		query.Set("limit", strconv.Itoa(size))
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, value, err := fetch[api.TxPage](c, "/entities/"+entity.ID+"/tx-events?"+query.Encode())
		if err != nil {
			return nil, err
		}
		obj, ok := value.(*jsonv.Object)
		if !ok {
			return nil, output.Errorf(output.Failure, "GET /entities/%s/tx-events: expected an object", entity.ID)
		}
		events, _ := obj.Get("events")
		assets, _ := obj.Get("assets")

		r.events = append(r.events, page.Events...)
		r.eventValues = append(r.eventValues, items(events)...)
		r.assets.Add(page.Assets)
		// Pages overlap in the assets they reference; each is listed once.
		for i, a := range page.Assets {
			if !seenAssets[a.ID] {
				seenAssets[a.ID] = true
				r.assetValues = append(r.assetValues, items(assets)[i])
			}
		}
		r.nextCursor = page.NextCursor
		if r.nextCursor == nil || *r.nextCursor == "" || (limit >= 0 && len(r.events) >= limit) || len(page.Events) == 0 {
			break
		}
		cursor = *r.nextCursor
	}
	if r.nextCursor != nil && *r.nextCursor == "" {
		r.nextCursor = nil
	}
	return r, nil
}

// resolveSourceID accepts a source's id or its label, as `sources sync` does.
func resolveSourceID(c *client.Client, entity api.Entity, needle string) (string, error) {
	sources, _, err := fetch[[]api.Source](c, "/entities/"+entity.ID+"/sources")
	if err != nil {
		return "", err
	}
	for _, s := range sources {
		if s.ID == needle || s.Label == needle {
			return s.ID, nil
		}
	}
	return "", output.Errorf(output.UsageError, "No source matching \"%s\" on %s.\nRun `grubless sources list --entity %s` to see them.", needle, entity.Name, entity.ID)
}

// resolveAssetID accepts an asset id, or a symbol the entity currently holds.
//
// Holdings are the only place the API maps symbols to ids, so an asset that
// has been fully sold needs its id — which --json shows on every leg. A
// symbol held on several chains is ambiguous and says so, rather than
// picking one: USDC on Solana and USDC on Ethereum are different histories.
func resolveAssetID(c *client.Client, entity api.Entity, needle string) (string, error) {
	if uuidPattern.MatchString(needle) {
		return needle, nil
	}
	holdings, _, err := fetch[[]api.Holding](c, "/entities/"+entity.ID+"/holdings")
	if err != nil {
		return "", err
	}
	var matches []api.Holding
	seen := map[string]bool{}
	for _, h := range holdings {
		if strings.EqualFold(h.Symbol, needle) && !seen[h.AssetID] {
			seen[h.AssetID] = true
			matches = append(matches, h)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].AssetID, nil
	case 0:
		return "", output.Errorf(output.UsageError,
			"%s holds no asset with symbol \"%s\".\nFor an asset it no longer holds, pass its id: `--json` shows the assetId on every leg.", entity.Name, needle)
	}
	lines := make([]string, len(matches))
	for i, h := range matches {
		lines[i] = fmt.Sprintf("  %s  %s", h.AssetID, output.HeldIn(h.Chain, h.SourceLabels(), 28))
	}
	return "", output.Errorf(output.UsageError, "\"%s\" matches %d assets on %s:\n%s\nUse the id.", needle, len(matches), entity.Name, strings.Join(lines, "\n"))
}

// document is one entity's result for --json: the events and assets as the
// server sent them, plus the filters that produced them — a filtered list
// with no record of its filter can't tell "nothing happened" from "nothing
// matched", the same reason `portfolio --json` echoes its range.
func (r *txResult) document(entity api.Entity, f TxFilters) jsonv.Value {
	opt := func(s string) jsonv.Value {
		if s == "" {
			return nil
		}
		return s
	}
	var limit jsonv.Value = defaultTxLimit
	switch {
	case f.Limit == "all":
		limit = "all"
	case f.Limit != "":
		n, _ := strconv.Atoi(f.Limit)
		limit = n
	}
	var next jsonv.Value
	if r.nextCursor != nil {
		next = *r.nextCursor
	}
	events := r.eventValues
	if events == nil {
		events = []jsonv.Value{}
	}
	assets := r.assetValues
	if assets == nil {
		assets = []jsonv.Value{}
	}
	return jsonv.Obj(
		"entity", entityRef(entity),
		"filters", jsonv.Obj(
			"category", opt(f.Category),
			"direction", opt(f.Direction),
			"from", opt(f.From),
			"to", opt(f.To),
			"search", opt(f.Search),
			"source", opt(f.Source),
			"asset", opt(f.Asset),
			"sort", opt(f.Sort),
			"limit", limit,
		),
		"events", events,
		"assets", assets,
		// Null when there's nothing more; otherwise where the next page
		// starts, for a script that wants to carry on from here.
		"nextCursor", next,
	)
}

func (r *txResult) print(f TxFilters) {
	if len(r.events) == 0 {
		// stderr, so an empty result piped anywhere stays empty.
		if f == (TxFilters{Limit: f.Limit, Sort: f.Sort}) {
			output.Note("No transactions yet.")
		} else {
			output.Note("No transactions match those filters.")
		}
		return
	}
	a := r.assets
	output.Table(r.events, []output.Column[api.TxEvent]{
		{Header: "ID", Value: func(e api.TxEvent) string { return txfmt.ShortID(e.ID) }},
		{Header: "DATE (UTC)", Value: func(e api.TxEvent) string { return txfmt.When(e.Ts) }},
		{Header: "TYPE", Value: txfmt.Type, MaxWidth: 26},
		{Header: "OUT", Value: func(e api.TxEvent) string { return txfmt.Legs(e, "out", a) }, AlignRight: true, MaxWidth: 30},
		{Header: "IN", Value: func(e api.TxEvent) string { return txfmt.Legs(e, "in", a) }, AlignRight: true, MaxWidth: 30},
		{Header: "FEE", Value: func(e api.TxEvent) string { return txfmt.Fee(e, a) }, AlignRight: true, MaxWidth: 24},
		{Header: "GAIN/LOSS", Value: func(e api.TxEvent) string { return output.Signed(txfmt.GainLoss(e)) }, AlignRight: true},
		{Header: "SOURCE", Value: txfmt.Source, MaxWidth: 20},
		{Header: "TAGS", Value: txfmt.Tags, MaxWidth: 24},
	})

	manual := false
	for _, e := range r.events {
		manual = manual || e.IsManuallyCategorized
	}
	if manual {
		output.Note(output.Dim("* categorised by hand"))
	}
	if r.nextCursor != nil {
		output.Note(output.Dim(fmt.Sprintf("Showing the first %d; more match. Raise --limit, or pass --limit all.", len(r.events))))
	}
}
