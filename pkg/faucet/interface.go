package faucet

import (
	"context"
)

// Fauceter defines the interface for a faucet client.
type Fauceter interface {
	FundAccountWithRetry(ctx context.Context, address string, amountBaseUnit float64, maxRetries int) (*FaucetResult, error)
	FundAccountWithRetryAndContext(ctx context.Context, address string, amountBaseUnit float64, maxRetries int, logCtx *LoggingContext) (*FaucetResult, error)
	FundAccountWithRetriesAndOptionsAndContext(ctx context.Context, address string, amountBaseUnit float64, opts *FundingOptions, logCtx *LoggingContext) (*FaucetResult, error)
}
