package faucet

import (
	"context"
)

// Fauceter defines the interface for a faucet client.
type Fauceter interface {
	FundAccountWeiWithRetry(ctx context.Context, address string, amountWei float64, maxRetries int) (*FaucetResult, error)
	FundAccountWeiWithRetryAndContext(ctx context.Context, address string, amountWei float64, maxRetries int, logCtx *LoggingContext) (*FaucetResult, error)
}
