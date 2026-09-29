package txfmt

import "strings"

// categoryLabels are the web app's names for transaction categories —
// "Cross-Chain Sell", not cross_chain_sell. Copied from the main repo's
// packages/core/src/transaction-categories.ts: the label strings only, since
// nothing of the tax engine may reach this public client (see
// scripts/verify-bundle.mjs). A category added there shows here as its key
// with spaces until this list catches up — the web's own fallback.
var categoryLabels = map[string]string{
	"buy":                         "Buy",
	"sell":                        "Sell",
	"cross_chain_buy":             "Cross-Chain Buy",
	"cross_chain_sell":            "Cross-Chain Sell",
	"send":                        "Send",
	"receive":                     "Receive",
	"transfer":                    "Transfer",
	"failed_in":                   "Failed (In)",
	"failed_out":                  "Failed (Out)",
	"ignore_in":                   "Ignore (In)",
	"ignore_out":                  "Ignore (Out)",
	"spam":                        "Spam",
	"dust_in":                     "Dust (In)",
	"dust_out":                    "Dust (Out)",
	"collateral_withdrawal":       "Collateral Withdrawal",
	"remove_liquidity":            "Remove Liquidity",
	"loan":                        "Loan",
	"receive_receipt_token":       "Receive Receipt Token",
	"staking_reward":              "Staking Reward",
	"voting_reward":               "Voting Reward",
	"staking_withdrawal":          "Staking Withdrawal",
	"instant_unstake":             "Instant Unstake",
	"staking_deactivation":        "Staking Deactivation",
	"staking_split":               "Staking Split",
	"staking_merge":               "Staking Merge",
	"collateral_deposit":          "Collateral Deposit",
	"add_liquidity":               "Add Liquidity",
	"loan_repayment":              "Loan Repayment",
	"loan_made":                   "Loan Made",
	"send_receipt_token":          "Send Receipt Token",
	"staking_deposit":             "Staking Deposit",
	"liquidation":                 "Liquidation",
	"equity_vest":                 "Share Vest",
	"equity_exercise":             "Option Exercise",
	"fiat_deposit":                "Fiat Deposit",
	"airdrop":                     "Airdrop",
	"chain_split":                 "Chain Split",
	"gift":                        "Gift",
	"sales":                       "Sales",
	"super_contribution_received": "Super Contribution Received",
	"income":                      "Income",
	"interest":                    "Interest",
	"mining":                      "Mining",
	"mint":                        "Mint",
	"loan_repayment_received":     "Loan Repayment Received",
	"rebate":                      "Rebate",
	"royalties":                   "Royalties",
	"fiat_withdrawal":             "Fiat Withdrawal",
	"burn":                        "Burn",
	"lost":                        "Lost",
	"outgoing_gift":               "Outgoing Gift",
	"personal_use":                "Personal Use",
	"stolen":                      "Stolen",
	"card_purchase":               "Card Purchase",
	"credit_purchase":             "Credit Purchase",
	"bill_payment":                "Bill Payment",
	"bank_fee":                    "Bank Fee",
	"atm_withdrawal":              "ATM Withdrawal",
	"refund":                      "Refund",
	"approval":                    "Approval",
	"expense":                     "Expense",
	"fee":                         "Fee",
	"staking_activation":          "Staking Activation",
	"wages":                       "Wages",
	"super_contribution":          "Super Contribution",
	"invoice_payment":             "Invoice Payment",
	"decrease_position":           "Decrease Position",
	"receive_position_token":      "Receive Position Token",
	"realized_profit":             "Realized Profit",
	"increase_position":           "Increase Position",
	"send_position_token":         "Send Position Token",
	"realized_loss":               "Realized Loss",
	"margin_fee":                  "Margin Fee",
	"incoming":                    "Incoming",
	"outgoing":                    "Outgoing",
	"unknown":                     "Unknown",
	"trade":                       "Trade",
	"wrapped_tokens":              "Wrapped Tokens",
	"reflection_tokens":           "Reflection Tokens",
	"rebase_tokens":               "Rebase Tokens",
	"cross_chain_trade":           "Cross-Chain Trade",
}

// CategoryLabel is a category as a person reads it.
func CategoryLabel(key string) string {
	if label, ok := categoryLabels[key]; ok {
		return label
	}
	return strings.ReplaceAll(key, "_", " ")
}
