/**
 * Wire shapes for the Grubless HTTP API — **this client's declared view of
 * them**, not the server's.
 *
 * The distinction matters and is the reason this file is a copy rather than an
 * import. A CLI talks to a *deployed* API over HTTP, at whatever version that
 * deployment happens to be running; the user's installed `grubless` and the
 * server it points at are separately versioned by construction. Compiling
 * against the server's own type declarations would therefore prove nothing
 * about the request actually in flight — it would only prove agreement with a
 * server we are not talking to. What catches real drift is
 * `src/test/e2e.test.ts`, which drives the built binary against a running
 * Grubless and fails on the wire.
 *
 * So: treat this as the contract this client *asserts*. When the API adds a
 * field, nothing here needs to change until the CLI wants it. When the API
 * changes one incompatibly, the e2e run is what tells us — and the
 * `x-grubless-min-cli-version` header (see `src/client.ts`) is what tells the
 * user.
 *
 * **Types only. No runtime exports, ever.** The tax engine is the moat, and
 * bundling any of it into a public npm package would hand it over. One
 * accidental *value* import is all it takes — `import type` erases at compile
 * time, a plain `import` does not — which is why this file has no imports of
 * its own and why `scripts/verify-bundle.mjs` greps the built bundle for engine
 * internals on every build. Keep both.
 *
 * All monetary/quantity fields are **decimal strings**, never numbers: these
 * are exact values from a Postgres `numeric`, and putting them through an
 * IEEE-754 double is how a tax figure quietly loses cents.
 */

// ---------- Entities ----------

/**
 * A union rather than `as const` over an array. The array form upstream is a
 * runtime export, which contradicts the no-runtime-exports rule above — it
 * survived there only because nothing ever value-imported it. Here the rule is
 * true by construction.
 */
export type EntityType =
  | "individual"
  | "sole_trader"
  | "partnership"
  | "smsf"
  | "trust"
  | "company"
  | "s_corp"
  | "c_corp";

export type EntityRole = "owner" | "preparer" | "viewer";

export interface Entity {
  id: string;
  name: string;
  entityType: EntityType;
  /** The requesting account's role on this entity, not the entity's own property. */
  role: EntityRole;
  createdAt: string;
}

// ---------- Sources ----------

export type SourceType = "on_chain" | "exchange_api" | "csv_import";
export type SyncStatus = "idle" | "syncing" | "error";

export interface Source {
  id: string;
  entityId: string;
  sourceType: SourceType;
  adapterKey: string;
  label: string;
  config: Record<string, unknown>;
  lastSyncedAt: string | null;
  syncStatus: SyncStatus;
  syncError: string | null;
  /** Pauses future syncing only — already-ingested data is untouched. */
  syncEnabled: boolean;
  createdAt: string;
  transactionCount: number;
  lastTransactionAt: string | null;
}

// ---------- Activity (what `--wait` polls) ----------

/** "queued" is written by the API at enqueue time — a job waiting behind the
 *  worker's concurrency limit is visible before it starts running. */
export type EntityActivityStatus = "queued" | "running" | "success" | "degraded" | "error";

export interface EntityActivity {
  id: string;
  entityId: string;
  sourceId: string | null;
  jobType: string;
  status: EntityActivityStatus;
  message: string | null;
  error: string | null;
  startedAt: string;
  updatedAt: string;
  finishedAt: string | null;
  source: { label: string } | null;
}

// ---------- Holdings ----------

export interface HoldingSource {
  sourceId: string;
  label: string;
  /** Derived by replaying ingested history. */
  calculatedQuantity: string;
  /** Queried live from the source; null where no live check exists (EVM, Hyperliquid, CSV). */
  reportedQuantity: string | null;
  /** reportedQuantity exists and differs beyond a dust tolerance. */
  mismatch: boolean;
}

export interface Holding {
  assetId: string;
  symbol: string;
  chain: string | null;
  imageUrl: string | null;
  quantity: string;
  /** In the entity's base currency; null when no cached price exists yet. */
  value: string | null;
  hasMismatch: boolean;
  isSpam: boolean;
  sources: HoldingSource[];
}

// ---------- Entity tax settings ----------

/**
 * The subset of `GET /entities/:id/tax-settings` the client actually reads.
 * The endpoint returns the whole settings row; the rest is server-side tax
 * configuration with no client meaning.
 */
export interface EntityTaxSettings {
  entityId: string;
  baseCurrency: string;
  /** 1-12. 7 for AU_ATO, 1 for US_IRS — never assume one. */
  financialYearStartMonth: number;
}

// ---------- Portfolio history ----------

export interface PortfolioHistoryPoint {
  /** YYYY-MM-DD. */
  date: string;
  value: string;
  /** Running total of income received to this date, in the entity's base currency. */
  cumulativeIncome: string;
  /** Market value of open lots minus their remaining cost basis — paper gain/loss. */
  unrealizedPL: string;
}

// ---------- Tax summary ----------

export interface TaxYearSummary {
  financialYear: string;
  /** Calendar year the FY starts in — the stable, URL-safe identifier. */
  startYear: number;
  startDate: string;
  endDate: string;
  currency: string;
  incomeLabel: string;
  income: string;
  rewardIncome: string;
  miningIncome: string;
  incomeByCategory: Record<string, string>;
  expenses: string;
  netIncome: string;
  rawCapitalGainLoss: string;
  bridgingGainLoss: string;
  wrappingGainLoss: string;
  capitalGainLossByCategory: Record<string, string>;
  discountedCapitalGainLoss: string;
  netCapitalGainLoss: string;
  feesPaid: string;
  feesByCategory: Record<string, string>;
  broughtForwardLoss: string;
  carriedForwardLoss: string;
  taxableAmount: string;
  taxPayable: string;
  rate: string | null;
  /** False for pass-through entities (trust/partnership) — no entity-level tax. */
  applicable: boolean;
  notes?: string;
}

// ---------- Warnings ----------

export interface ZeroCostWarning {
  disposalId: string;
  txEventId: string;
  eventType: string;
  ts: string;
  assetSymbol: string;
  assetChain: string | null;
  sourceLabel: string;
  sourceAdapterKey: string;
  quantity: string;
  proceedsAmount: string;
  gainLossAmount: string;
  currency: string;
}

export interface UncategorizedTransferWarning {
  txEventId: string;
  eventType: string;
  ts: string;
  direction: "in" | "out";
  assetSymbol: string;
  assetChain: string | null;
  amount: string;
}

export interface UnpricedAssetWarning {
  assetId: string;
  symbol: string;
  chain: string | null;
  contractOrMintAddress: string | null;
  legCount: number;
  incomeLegs: number;
  netQuantity: string;
  firstSeen: string;
  lastSeen: string;
}

export interface UnbalancedTransferWarning {
  sourceId: string;
  sourceLabel: string;
  sourceAdapterKey: string;
  assetId: string;
  assetSymbol: string;
  assetChain: string | null;
  netQuantity: string;
  inCount: number;
  outCount: number;
  firstSeen: string;
  lastSeen: string;
}

// ---------- API tokens ----------

export type ApiTokenScope = "read" | "write";

export interface ApiToken {
  id: string;
  name: string;
  scope: ApiTokenScope;
  lastUsedAt: string | null;
  expiresAt: string | null;
  createdAt: string;
}

/** `token` is populated on the creation response only, and never again. */
export interface CreatedApiToken extends ApiToken {
  token: string;
}

// ---------- Billing ----------

export interface Entitlements {
  /** Maximum entities the account may OWN; null = unlimited. */
  maxOwnedEntities: number | null;
  /** Whether `/entities/:id/reports/*` may be produced — the paid deliverable. */
  canDownloadReports: boolean;
}

/** `GET /billing/me`. */
export interface AccountBilling {
  /** False means this deployment meters nobody — show nothing about plans. */
  billingEnabled: boolean;
  plan: string;
  planLabel: string;
  /** Set only when a subscription lapsed, i.e. differs from the effective plan. */
  lapsedFrom: string | null;
  status: string | null;
  entitlements: Entitlements;
  usage: { ownedEntities: number };
}
