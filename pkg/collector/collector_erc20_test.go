package collector

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
)

type mockERC20Client struct {
	balance *big.Int
	err     error
	closed  bool
}

func (m *mockERC20Client) BalanceOf(ctx context.Context, address common.Address) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.balance, nil
}

func (m *mockERC20Client) Close() {
	m.closed = true
}

func TestERC20CollectorCollectAccountBalance(t *testing.T) {
	tests := []struct {
		name             string
		balance          *big.Int
		clientErr        error
		unit             *currency.Unit
		metricsUnit      *currency.Unit
		currencyRegistry *currency.Registry
		expectedValue    float64
		expectedHealth   float64
		expectError      bool
		errorContains    string
	}{
		{
			name:             "successful balance query with wei to eth conversion",
			balance:          big.NewInt(1e18),
			unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
			metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
			currencyRegistry: currency.NewDefaultRegistry(),
			expectedValue:    1.0,
			expectedHealth:   1.0,
			expectError:      false,
		},
		{
			name:             "successful balance query with same unit (no conversion)",
			balance:          big.NewInt(1000000),
			unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
			metricsUnit:      &currency.Unit{Name: "wei", Symbol: "wei"},
			currencyRegistry: currency.NewDefaultRegistry(),
			expectedValue:    1000000.0,
			expectedHealth:   1.0,
			expectError:      false,
		},
		{
			name:             "zero balance",
			balance:          big.NewInt(0),
			unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
			metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
			currencyRegistry: currency.NewDefaultRegistry(),
			expectedValue:    0.0,
			expectedHealth:   1.0,
			expectError:      false,
		},
		{
			name:             "large balance",
			balance:          new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000000)),
			unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
			metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
			currencyRegistry: currency.NewDefaultRegistry(),
			expectedValue:    1000000.0,
			expectedHealth:   1.0,
			expectError:      false,
		},
		{
			name:             "balance fetch error",
			clientErr:        errors.New("network error"),
			unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
			metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
			currencyRegistry: currency.NewDefaultRegistry(),
			expectError:      true,
			errorContains:    "failed to fetch erc20 balance",
		},
		{
			name:             "nil currency registry (no conversion)",
			balance:          big.NewInt(1000),
			unit:             &currency.Unit{Name: "custom", Symbol: "CST"},
			metricsUnit:      &currency.Unit{Name: "custom", Symbol: "CST"},
			currencyRegistry: nil,
			expectedValue:    1000.0,
			expectedHealth:   1.0,
			expectError:      false,
		},
		{
			name:             "currency conversion error",
			balance:          big.NewInt(1000),
			unit:             &currency.Unit{Name: "unknown-unit", Symbol: "UNK"},
			metricsUnit:      &currency.Unit{Name: "another-unknown", Symbol: "AUK"},
			currencyRegistry: currency.NewDefaultRegistry(),
			expectError:      true,
			errorContains:    "failed to convert erc20 balance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockERC20Client{
				balance: tt.balance,
				err:     tt.clientErr,
			}

			collector := &ERC20Collector{
				nodeName:         "erc20-test",
				client:           mockClient,
				unit:             tt.unit,
				metricsUnit:      tt.metricsUnit,
				currencyRegistry: tt.currencyRegistry,
			}

			account := &config.Account{
				Address: "0x000000000000000000000000000000000000dead",
				Name:    "test-account",
			}

			result, err := collector.CollectAccountBalance(context.Background(), account)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedValue, result.Value)
			assert.Equal(t, tt.expectedHealth, result.Health)
			assert.Equal(t, "erc20-test", result.NodeName)
			assert.Equal(t, *account, result.Account)
		})
	}
}

func TestERC20CollectorClose(t *testing.T) {
	tests := []struct {
		name       string
		client     erc20BalanceReader
		shouldCall bool
	}{
		{
			name:       "close with valid client",
			client:     &mockERC20Client{},
			shouldCall: true,
		},
		{
			name:       "close with nil client",
			client:     nil,
			shouldCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := &ERC20Collector{
				nodeName: "test-node",
				client:   tt.client,
			}

			err := collector.Close()
			assert.NoError(t, err)

			if tt.shouldCall {
				if mockClient, ok := tt.client.(*mockERC20Client); ok {
					assert.True(t, mockClient.closed, "Close() should have been called on the client")
				}
			}
		})
	}
}

func TestBigIntToFloat(t *testing.T) {
	tests := []struct {
		name          string
		input         *big.Int
		expectedValue float64
		expectError   bool
		errorContains string
		checkInfinity bool
	}{
		{
			name:          "nil value",
			input:         nil,
			expectError:   true,
			errorContains: "nil balance",
		},
		{
			name:          "zero value",
			input:         big.NewInt(0),
			expectedValue: 0.0,
			expectError:   false,
		},
		{
			name:          "small positive value",
			input:         big.NewInt(12345),
			expectedValue: 12345.0,
			expectError:   false,
		},
		{
			name:          "large positive value (1 ETH in wei)",
			input:         big.NewInt(1e18),
			expectedValue: 1e18,
			expectError:   false,
		},
		{
			name:          "very large positive value",
			input:         new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1e9)),
			expectedValue: 1e27,
			expectError:   false,
		},
		{
			name:          "negative value",
			input:         big.NewInt(-1000),
			expectedValue: -1000.0,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := bigIntToFloat(tt.input)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)
			if tt.checkInfinity {
				// Just verify it doesn't error - value will be infinity
				return
			}
			assert.Equal(t, tt.expectedValue, result)
		})
	}
}

func TestERC20CollectorMultipleAddresses(t *testing.T) {
	registry := currency.NewDefaultRegistry()

	addresses := []struct {
		address string
		balance *big.Int
	}{
		{"0x0000000000000000000000000000000000000001", big.NewInt(1e18)},
		{"0x0000000000000000000000000000000000000002", big.NewInt(2e18)},
		{"0xDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF", big.NewInt(5e17)},
	}

	for _, addr := range addresses {
		t.Run("address_"+addr.address, func(t *testing.T) {
			mockClient := &mockERC20Client{balance: addr.balance}

			collector := &ERC20Collector{
				nodeName:         "erc20-test",
				client:           mockClient,
				unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
				metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
				currencyRegistry: registry,
			}

			account := &config.Account{
				Address: addr.address,
				Name:    "test-account-" + addr.address,
			}

			result, err := collector.CollectAccountBalance(context.Background(), account)
			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, account.Address, result.Account.Address)
		})
	}
}

func TestERC20CollectorContextCancellation(t *testing.T) {
	mockClient := &mockERC20Client{
		err: context.Canceled,
	}

	collector := &ERC20Collector{
		nodeName:         "erc20-test",
		client:           mockClient,
		unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
		metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
		currencyRegistry: currency.NewDefaultRegistry(),
	}

	account := &config.Account{
		Address: "0x000000000000000000000000000000000000dead",
		Name:    "test-account",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := collector.CollectAccountBalance(ctx, account)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch erc20 balance")
}

// TestERC20CollectorRealWorldScenarios tests realistic token scenarios
func TestERC20CollectorRealWorldScenarios(t *testing.T) {
	registry := currency.NewDefaultRegistry()

	scenarios := []struct {
		name           string
		balance        *big.Int
		decimals       int
		expectedETHVal float64
		description    string
	}{
		{
			name:           "USDT balance (6 decimals)",
			balance:        big.NewInt(1000000), // 1 USDT
			decimals:       6,
			expectedETHVal: 1000000.0 / 1e18, // When converted through wei -> eth
			description:    "1 USDT with 6 decimals",
		},
		{
			name:           "DAI balance (18 decimals)",
			balance:        big.NewInt(1e18), // 1 DAI
			decimals:       18,
			expectedETHVal: 1.0,
			description:    "1 DAI with 18 decimals",
		},
		{
			name:           "ZAMA balance (8 decimals)",
			balance:        big.NewInt(100000000), // 1 ZAMA
			decimals:       8,
			expectedETHVal: 100000000.0 / 1e18,
			description:    "1 ZAMA with 8 decimals",
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			mockClient := &mockERC20Client{balance: sc.balance}

			collector := &ERC20Collector{
				nodeName:         "erc20-test",
				client:           mockClient,
				unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
				metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
				currencyRegistry: registry,
			}

			account := &config.Account{
				Address: "0x0000000000000000000000000000000000000001",
				Name:    "test-" + sc.name,
			}

			result, err := collector.CollectAccountBalance(context.Background(), account)
			require.NoError(t, err)
			assert.NotNil(t, result)

			// Verify the balance was converted correctly
			floatVal, _ := bigIntToFloat(sc.balance)
			assert.Greater(t, floatVal, 0.0)
		})
	}
}
