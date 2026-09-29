// Package api is this client's declared view of the Grubless API's wire
// shapes — a copy of src/api-types.ts, for the same reason that file is a
// copy rather than an import: the CLI and the deployment it talks to are
// versioned separately, so agreeing with the server's own declarations would
// prove nothing about the request in flight.
//
// These structs are for *rendering tables* only. Every `--json` output goes
// through internal/jsonv instead, so a field this file doesn't declare still
// reaches a script unchanged.
//
// Nullable fields are pointers: the TS renders null as "—" and a value as a
// value, and "no price cached yet" must not look like "worth nothing".
// Amounts are decimal strings, never float64.
package api

import (
	"encoding/json"
	"fmt"

	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
)

type Entity struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	EntityType string `json:"entityType"`
	Role       string `json:"role"`
	CreatedAt  string `json:"createdAt"`
}

type Source struct {
	ID           string  `json:"id"`
	EntityID     string  `json:"entityId"`
	SourceType   string  `json:"sourceType"`
	AdapterKey   string  `json:"adapterKey"`
	Label        string  `json:"label"`
	LastSyncedAt *string `json:"lastSyncedAt"`
	SyncStatus   string  `json:"syncStatus"`
	SyncError    *string `json:"syncError"`
	SyncEnabled  bool    `json:"syncEnabled"`
	// Pointer so an absent count prints "undefined", as String(undefined)
	// does in the TS, rather than a confident 0.
	TransactionCount *float64 `json:"transactionCount"`
}

type EntityActivity struct {
	ID        string  `json:"id"`
	EntityID  string  `json:"entityId"`
	SourceID  *string `json:"sourceId"`
	JobType   string  `json:"jobType"`
	Status    string  `json:"status"`
	Message   *string `json:"message"`
	Error     *string `json:"error"`
	StartedAt string  `json:"startedAt"`
	Source    *struct {
		Label *string `json:"label"`
	} `json:"source"`
}

type HoldingSource struct {
	SourceID string `json:"sourceId"`
	Label    string `json:"label"`
}

type Holding struct {
	AssetID     string          `json:"assetId"`
	Symbol      string          `json:"symbol"`
	Chain       *string         `json:"chain"`
	Quantity    *string         `json:"quantity"`
	Value       *string         `json:"value"`
	HasMismatch bool            `json:"hasMismatch"`
	IsSpam      bool            `json:"isSpam"`
	Sources     []HoldingSource `json:"sources"`
}

// SourceLabels is `holding.sources?.map(s => s.label) ?? []`.
func (h Holding) SourceLabels() []string {
	labels := make([]string, len(h.Sources))
	for i, s := range h.Sources {
		labels[i] = s.Label
	}
	return labels
}

type EntityTaxSettings struct {
	EntityID                string   `json:"entityId"`
	BaseCurrency            string   `json:"baseCurrency"`
	FinancialYearStartMonth *float64 `json:"financialYearStartMonth"`
}

// FYStartMonth is `settings?.financialYearStartMonth ?? 7` — AU's July start
// only when the entity has no settings at all.
func (s *EntityTaxSettings) FYStartMonth() int {
	if s == nil || s.FinancialYearStartMonth == nil {
		return 7
	}
	return int(*s.FinancialYearStartMonth)
}

type PortfolioHistoryPoint struct {
	Date             string  `json:"date"`
	Value            *string `json:"value"`
	CumulativeIncome *string `json:"cumulativeIncome"`
	UnrealizedPL     *string `json:"unrealizedPL"`
}

type TaxYearSummary struct {
	FinancialYear      string   `json:"financialYear"`
	StartYear          *float64 `json:"startYear"`
	Currency           string   `json:"currency"`
	IncomeLabel        string   `json:"incomeLabel"`
	Income             *string  `json:"income"`
	Expenses           *string  `json:"expenses"`
	NetCapitalGainLoss *string  `json:"netCapitalGainLoss"`
	TaxableAmount      *string  `json:"taxableAmount"`
	TaxPayable         *string  `json:"taxPayable"`
	CarriedForwardLoss *string  `json:"carriedForwardLoss"`
	Applicable         bool     `json:"applicable"`
}

type ZeroCostWarning struct {
	Ts             string  `json:"ts"`
	AssetSymbol    string  `json:"assetSymbol"`
	SourceLabel    string  `json:"sourceLabel"`
	Quantity       *string `json:"quantity"`
	ProceedsAmount *string `json:"proceedsAmount"`
}

type UncategorizedTransferWarning struct {
	Ts          string  `json:"ts"`
	Direction   string  `json:"direction"`
	AssetSymbol string  `json:"assetSymbol"`
	Amount      *string `json:"amount"`
}

// Decode reads a response body into a table struct.
func Decode[T any](raw []byte) (T, error) {
	var v T
	err := json.Unmarshal([]byte(jsstr.DecodeUTF8(raw, true)), &v)
	if err != nil {
		return v, fmt.Errorf("unexpected response shape: %w", err)
	}
	return v, nil
}

// Value parses a response body for passthrough to `--json`.
func Value(raw []byte) (jsonv.Value, error) {
	return jsonv.Parse([]byte(jsstr.DecodeUTF8(raw, true)))
}

// ---------- Transactions (GET /entities/:id/tx-events) ----------

// TxPage is one page of transactions. Assets referenced by the page's legs
// arrive alongside rather than inline, so a leg's symbol is a lookup.
type TxPage struct {
	Events     []TxEvent `json:"events"`
	Assets     []TxAsset `json:"assets"`
	NextCursor *string   `json:"nextCursor"`
}

type TxEvent struct {
	ID                    string  `json:"id"`
	EventType             string  `json:"eventType"`
	Ts                    string  `json:"ts"`
	IsManuallyCategorized bool    `json:"isManuallyCategorized"`
	IsInternalTransfer    bool    `json:"isInternalTransfer"`
	Description           *string `json:"description"`
	Notes                 *string `json:"notes"`
	DetectedProtocol      *string `json:"detectedProtocol"`
	TaxTreatment          *string `json:"taxTreatment"`
	Legs                  []TxLeg `json:"legs"`
	Source                *struct {
		Label string `json:"label"`
	} `json:"source"`
	Tags []struct {
		Label string `json:"label"`
	} `json:"tags"`
}

type TxLeg struct {
	AssetID             string  `json:"assetId"`
	Direction           string  `json:"direction"`
	Role                string  `json:"role"`
	Amount              *string `json:"amount"`
	Value               *string `json:"value"`
	Currency            string  `json:"currency"`
	Proceeds            *string `json:"proceeds"`
	CostBasis           *string `json:"costBasis"`
	GainLoss            *string `json:"gainLoss"`
	CounterpartyAddress *string `json:"counterpartyAddress"`
}

type TxAsset struct {
	ID     string  `json:"id"`
	Symbol string  `json:"symbol"`
	Chain  *string `json:"chain"`
}
