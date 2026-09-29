package commands

import (
	"strings"

	"github.com/grubless/grubless-cli/internal/api"
	"github.com/grubless/grubless-cli/internal/client"
	"github.com/grubless/grubless-cli/internal/jsonv"
	"github.com/grubless/grubless-cli/internal/jsstr"
	"github.com/grubless/grubless-cli/internal/output"
)

func TaxSummary(c *client.Client, s Scope, year string) (int, error) {
	entities, err := ResolveScope(c, s)
	if err != nil {
		return 0, err
	}
	multi := len(entities) > 1

	for _, entity := range entities {
		all, value, err := fetch[[]api.TaxYearSummary](c, "/entities/"+entity.ID+"/tax-summary")
		if err != nil {
			return 0, err
		}
		// startYear is the stable identifier; financialYear is a display
		// label with an en dash in it that nobody will type correctly.
		var rows []api.TaxYearSummary
		values := []jsonv.Value{}
		for i, r := range all {
			if year == "" || countString(r.StartYear) == year {
				rows = append(rows, r)
				values = append(values, items(value)[i])
			}
		}

		if s.JSON {
			output.JSON(jsonv.Obj("entity", entityRef(entity), "summaries", values))
			continue
		}

		if multi {
			output.Out(output.Bold(entity.Name + " (" + entity.EntityType + ")"))
		}
		if len(rows) == 0 {
			if year != "" {
				output.Note("No tax summary for " + year + ".")
			} else {
				output.Note("No tax summary yet.")
			}
			continue
		}

		// The income column's name varies by entity structure.
		output.Table(rows, []output.Column[api.TaxYearSummary]{
			{Header: "FY", Value: func(r api.TaxYearSummary) string { return r.FinancialYear }},
			{Header: strings.ToUpper(rows[0].IncomeLabel), Value: func(r api.TaxYearSummary) string { return output.Money(r.Income) }, AlignRight: true},
			{Header: "EXPENSES", Value: func(r api.TaxYearSummary) string { return output.Money(r.Expenses) }, AlignRight: true},
			{Header: "NET CGT", Value: func(r api.TaxYearSummary) string { return output.Money(r.NetCapitalGainLoss) }, AlignRight: true},
			{Header: "TAXABLE", Value: func(r api.TaxYearSummary) string { return output.Money(r.TaxableAmount) }, AlignRight: true},
			// A pass-through entity has no entity-level tax; "0.00" would
			// read as "nothing owed" rather than "not computed here".
			{Header: "TAX", Value: func(r api.TaxYearSummary) string {
				if r.Applicable {
					return output.Money(r.TaxPayable)
				}
				return "n/a (pass-through)"
			}, AlignRight: true},
			{Header: "CCY", Value: func(r api.TaxYearSummary) string { return strings.ToUpper(r.Currency) }},
		})

		for _, r := range rows {
			// `loss !== "0" && Number(loss) !== 0`: null is Number 0, so a
			// missing figure is skipped rather than printed as a loss.
			if r.CarriedForwardLoss == nil || *r.CarriedForwardLoss == "0" || jsstr.ParseNumber(*r.CarriedForwardLoss) == 0 {
				continue
			}
			output.Note(output.Dim("  " + r.FinancialYear + ": " + output.Money(r.CarriedForwardLoss) + " capital loss carried forward"))
		}
		if multi {
			output.Out("")
		}
	}
	return output.Ok, nil
}
