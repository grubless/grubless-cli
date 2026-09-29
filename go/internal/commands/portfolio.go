package commands

import (
	"strings"
	"time"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/chart"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
	"github.com/grubless/grubless-cli/go/internal/timerange"
)

// `grubless portfolio` — a chart for a person, every daily point for a
// script. Defaults to `all`, unlike the interactive surfaces: a pipe gets
// everything unless told otherwise. See src/commands/portfolio.ts.

const (
	chartRows    = 16
	defaultWidth = 80
	// Below this the plot is narrower than its own axis labels.
	minWidth = 40
)

// historyPoint is one point in both views.
type historyPoint struct {
	api.PortfolioHistoryPoint
	value jsonv.Value
}

func Portfolio(c *client.Client, s Scope, rangeFlag string) (int, error) {
	r, err := parseRange(rangeFlag)
	if err != nil {
		return 0, err
	}
	entities, err := ResolveScope(c, s)
	if err != nil {
		return 0, err
	}

	var documents []jsonv.Value
	for i, entity := range entities {
		// Both requests at once, as the TS's Promise.all. Settings matter only
		// for "fy", and an entity without them 404s — not worth failing over.
		type settingsResult struct{ settings *api.EntityTaxSettings }
		settingsCh := make(chan settingsResult, 1)
		if r == "fy" {
			go func() {
				st, _, err := fetch[api.EntityTaxSettings](c, "/entities/"+entity.ID+"/tax-settings")
				if err != nil {
					settingsCh <- settingsResult{nil}
					return
				}
				settingsCh <- settingsResult{&st}
			}()
		} else {
			settingsCh <- settingsResult{nil}
		}

		all, value, err := fetch[[]api.PortfolioHistoryPoint](c, "/entities/"+entity.ID+"/portfolio-history")
		if err != nil {
			return 0, err
		}
		settings := (<-settingsCh).settings

		rows := make([]historyPoint, len(all))
		for j, p := range all {
			rows[j] = historyPoint{p, items(value)[j]}
		}
		points := timerange.Filter(rows, func(p historyPoint) string { return p.Date }, r, settings.FYStartMonth(), time.Now())

		if s.JSON {
			// The range is echoed back: a filtered array with no record of the
			// filter can't tell a quiet year from a narrow window.
			values := make([]jsonv.Value, len(points))
			for j, p := range points {
				values[j] = p.value
			}
			documents = append(documents, jsonv.Obj("entity", entityRef(entity), "range", string(r), "points", values))
			continue
		}

		if i > 0 {
			output.Out("")
		}
		printChart(entity, points, len(entities) > 1, len(all) > 0, r)
	}

	if s.JSON {
		output.JSON(pick(s.AllEntities, documents))
	}
	return output.Ok, nil
}

func parseRange(value string) (timerange.Key, error) {
	if value == "" {
		return "all", nil
	}
	if !timerange.IsKey(value) {
		keys := make([]string, len(timerange.Keys))
		for i, k := range timerange.Keys {
			keys[i] = string(k)
		}
		return "", output.Errorf(output.UsageError, "Unknown range \"%s\".\nAvailable: %s", value, strings.Join(keys, ", "))
	}
	return timerange.Key(value), nil
}

func printChart(entity api.Entity, points []historyPoint, showName, hasAnyHistory bool, r timerange.Key) {
	if showName {
		output.Out(output.Bold(entity.Name))
	}
	if len(points) == 0 {
		// Two different facts: no history at all needs a source connected;
		// history that predates the window needs a wider --range.
		if hasAnyHistory {
			output.Out(output.Dim("No portfolio history in the last " + string(r) + " — try --range all."))
		} else {
			output.Out(output.Dim("No portfolio history yet."))
		}
		return
	}
	last := points[len(points)-1]
	output.Out(output.Dim(last.Date + "   VALUE " + output.Money(last.Value) +
		"   UNREALISED " + output.Signed(last.UnrealizedPL) +
		"   INCOME " + output.Money(last.CumulativeIncome)))

	width := defaultWidth
	if cols, ok := output.Columns(); ok {
		width = cols
	}
	history := make([]api.PortfolioHistoryPoint, len(points))
	for i, p := range points {
		history[i] = p.PortfolioHistoryPoint
	}
	lines := chart.Render(chart.FromHistory(history), chart.Options{Width: max(minWidth, width), Height: chartRows, Colour: output.UseColour})
	for _, line := range lines {
		output.Out(jsstr.TrimEnd(line))
	}
}
