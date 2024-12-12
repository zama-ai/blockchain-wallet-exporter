package collector

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
)

// MockModuleCollector implements IModuleCollector for testing
type MockModuleCollector struct {
	CollectAccountBalanceFunc func(ctx context.Context, account *config.Account) (*BaseResult, error)
	CloseFunc                 func() error
}

func (m *MockModuleCollector) CollectAccountBalance(ctx context.Context, account *config.Account) (*BaseResult, error) {
	if m.CollectAccountBalanceFunc != nil {
		return m.CollectAccountBalanceFunc(ctx, account)
	}
	return nil, nil
}

func (m *MockModuleCollector) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func TestBaseCollector_CollectMetrics(t *testing.T) {
	// Create registry at test start
	registry := currency.NewDefaultRegistry()

	tests := []struct {
		name          string
		accounts      []*config.Account
		mockResults   []*BaseResult
		expectedError bool
		timeout       time.Duration
	}{
		{
			name: "successful collection",
			accounts: []*config.Account{
				{
					Name:    "test-account-1",
					Address: "address-1",
				},
				{
					Name:    "test-account-2",
					Address: "address-2",
				},
			},
			mockResults: []*BaseResult{
				{
					NodeName: "test-node",
					Account: config.Account{
						Name:    "test-account-1",
						Address: "address-1",
					},
					Value:  1.0,
					Health: 1.0,
				},
				{
					NodeName: "test-node",
					Account: config.Account{
						Name:    "test-account-2",
						Address: "address-2",
					},
					Value:  2.0,
					Health: 1.0,
				},
			},
			timeout:       5 * time.Second,
			expectedError: false,
		},
		{
			name: "collection with timeout",
			accounts: []*config.Account{
				{
					Name:    "test-account-1",
					Address: "address-1",
				},
			},
			mockResults:   nil,
			timeout:       1 * time.Millisecond, // Very short timeout to trigger timeout error
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProcessor := &MockModuleCollector{
				CollectAccountBalanceFunc: func(ctx context.Context, account *config.Account) (*BaseResult, error) {
					// Simulate work
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					default:
						for i, acc := range tt.accounts {
							if acc.Address == account.Address {
								if tt.mockResults != nil {
									return tt.mockResults[i], nil
								}
							}
						}
						return &BaseResult{
							NodeName: "test-node",
							Account:  *account,
							Value:    1.0,
							Health:   1.0,
						}, nil
					}
				},
			}

			node := &config.Node{
				Name:        "test-node",
				Module:      "evm",
				Unit:        registry.MustGet("ETH"),
				MetricsUnit: registry.MustGet("WEI"),
				Accounts:    tt.accounts,
				Labels: map[string]string{
					"network": "testnet",
				},
			}

			collector := NewBaseCollector(node, mockProcessor, WithCollectorTimeout(tt.timeout))

			// Ensure collector is of the correct type
			baseCollector, ok := collector.(*BaseCollector)
			if !ok {
				t.Fatalf("collector is not of type *BaseCollector")
			}

			results := baseCollector.collectMetrics()

			assert.Len(t, results, len(tt.accounts))

			sort.Slice(results, func(i, j int) bool {
				return results[i].Account.Address < results[j].Account.Address
			})
			sort.Slice(tt.accounts, func(i, j int) bool {
				return tt.accounts[i].Address < tt.accounts[j].Address
			})

			// Verify results
			for i, result := range results {
				t.Logf("result: %v", result)
				assert.Equal(t, tt.accounts[i].Address, result.Account.Address)
				assert.Equal(t, "test-node", result.NodeName)
			}
		})
	}
}
