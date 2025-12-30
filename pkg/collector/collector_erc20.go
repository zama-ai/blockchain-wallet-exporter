package collector

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/erc20"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

type erc20BalanceReader interface {
	BalanceOf(ctx context.Context, address common.Address) (*big.Int, error)
	Close()
}

// ERC20Collector implements the IModuleCollector for ERC20 token balances.
type ERC20Collector struct {
	nodeName         string
	client           erc20BalanceReader
	labels           map[string]string
	unit             *currency.Unit
	metricsUnit      *currency.Unit
	currencyRegistry *currency.Registry
}

func NewERC20Collector(nodeConfig *config.Node, currencyRegistry *currency.Registry, opts ...CollectorOption) (prometheus.Collector, error) {
	if nodeConfig.Unit == nil {
		return nil, fmt.Errorf("node %s missing base unit configuration", nodeConfig.Name)
	}
	if nodeConfig.MetricsUnit == nil {
		return nil, fmt.Errorf("node %s missing metrics unit configuration", nodeConfig.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := erc20.NewClient(ctx, nodeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to init erc20 client: %w", err)
	}

	collector := &ERC20Collector{
		nodeName:         nodeConfig.Name,
		client:           client,
		labels:           nodeConfig.Labels,
		unit:             nodeConfig.Unit,
		metricsUnit:      nodeConfig.MetricsUnit,
		currencyRegistry: currencyRegistry,
	}

	options := []CollectorOption{
		WithCollectorTimeout(5 * time.Second),
	}
	options = append(options, opts...)

	return NewBaseCollector(nodeConfig, collector, options...), nil
}

func (ec *ERC20Collector) CollectAccountBalance(ctx context.Context, account *config.Account) (*BaseResult, error) {
	address := common.HexToAddress(account.Address)
	rawBalance, err := ec.client.BalanceOf(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch erc20 balance for %s: %w", account.Address, err)
	}

	floatVal, err := bigIntToFloat(rawBalance)
	if err != nil {
		return nil, err
	}

	converted := floatVal
	if ec.currencyRegistry != nil && ec.metricsUnit != nil && ec.unit != nil {
		converted, err = ec.currencyRegistry.Convert(floatVal, ec.unit.Name, ec.metricsUnit.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to convert erc20 balance for %s: %w", account.Address, err)
		}
	}

	logger.Debugf("erc20 balance for %s: %f %s (converted to %f %s)", account.Address, floatVal, ec.unit.Symbol, converted, ec.metricsUnit.Symbol)

	return &BaseResult{
		NodeName: ec.nodeName,
		Account:  *account,
		Value:    converted,
		Health:   1.0,
	}, nil
}

func (ec *ERC20Collector) Close() error {
	if ec.client != nil {
		ec.client.Close()
		logger.Debugf("closed erc20 client for node %s", ec.nodeName)
	}
	return nil
}

func bigIntToFloat(value *big.Int) (float64, error) {
	if value == nil {
		return 0, fmt.Errorf("nil balance")
	}
	amount := new(big.Float).SetInt(value)
	if amount.IsInf() {
		return 0, fmt.Errorf("balance too large for float64 representation")
	}
	result, _ := amount.Float64()
	return result, nil
}
