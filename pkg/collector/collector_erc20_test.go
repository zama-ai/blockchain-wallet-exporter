package collector

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
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
	registry := currency.NewDefaultRegistry()
	mockClient := &mockERC20Client{balance: big.NewInt(1e18)}

	collector := &ERC20Collector{
		nodeName:         "erc20-test",
		client:           mockClient,
		unit:             &currency.Unit{Name: "wei", Symbol: "wei"},
		metricsUnit:      &currency.Unit{Name: "eth", Symbol: "ETH"},
		currencyRegistry: registry,
	}

	account := &config.Account{
		Address: "0x000000000000000000000000000000000000dead",
		Name:    "test-account",
	}

	result, err := collector.CollectAccountBalance(context.Background(), account)
	assert.NoError(t, err)
	assert.Equal(t, 1.0, result.Value)
	assert.Equal(t, 1.0, result.Health)
}

func TestERC20CollectorCollectAccountBalanceError(t *testing.T) {
	mockErr := errors.New("balance error")
	mockClient := &mockERC20Client{err: mockErr}

	collector := &ERC20Collector{
		nodeName:    "erc20-test",
		client:      mockClient,
		unit:        &currency.Unit{Name: "wei", Symbol: "wei"},
		metricsUnit: &currency.Unit{Name: "wei", Symbol: "wei"},
	}

	account := &config.Account{
		Address: "0x000000000000000000000000000000000000dead",
		Name:    "test-account",
	}

	_, err := collector.CollectAccountBalance(context.Background(), account)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch erc20 balance")
}
