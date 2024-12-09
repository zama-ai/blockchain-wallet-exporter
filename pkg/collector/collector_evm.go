package collector

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

// EVMCollector implements the IModuleCollectorV2 interface for Ethereum-compatible chains
type EVMCollector struct {
	nodeName string
	client   *ethclient.Client
	//accounts         []*config.Account
	labels           map[string]string
	unit             *currency.Unit
	currencyRegistry *currency.Registry
}

// EVMCollectorOption defines functional options for EVMCollector
type EVMCollectorOption func(*EVMCollector)

func WithEVMLabels(labels map[string]string) EVMCollectorOption {
	return func(ec *EVMCollector) {
		ec.labels = labels
	}
}

func WithCurrencyRegistry(registry *currency.Registry) EVMCollectorOption {
	return func(ec *EVMCollector) {
		ec.currencyRegistry = registry
	}
}

func NewEVMCollector(nodeConfig config.Node, currencyRegistry *currency.Registry, opts ...EVMCollectorOption) (prometheus.Collector, error) {
	// Configure TLS based on node configuration
	tlsConfig := &tls.Config{InsecureSkipVerify: nodeConfig.HttpSSLVerify == "false"}
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   10 * time.Second,
	}

	// Setup RPC client with optional authorization
	rpcClient, err := rpc.DialOptions(context.Background(), nodeConfig.HttpAddr, rpc.WithHTTPClient(httpClient), rpc.WithHTTPAuth(func(h http.Header) error {
		if auth := nodeConfig.Authorization; auth != nil {
			h.Set("Authorization", fmt.Sprintf("Basic %s", auth.Username+":"+auth.Password))
		}
		return nil
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ethereum node: %w", err)
	}

	// Initialize EVMCollector
	evmCollector := &EVMCollector{
		nodeName:         nodeConfig.Name,
		client:           ethclient.NewClient(rpcClient),
		labels:           nodeConfig.Labels,
		unit:             nodeConfig.Unit,
		currencyRegistry: currencyRegistry,
	}

	// Apply functional options
	for _, opt := range opts {
		opt(evmCollector)
	}

	return NewBaseCollector(
		nodeConfig,
		evmCollector,
		WithCollectorTimeout(10*time.Second),
	), nil
}

// CollectAccountBalance implements IModuleCollectorV2 interface
func (ec *EVMCollector) CollectAccountBalance(ctx context.Context, account *config.Account) (*BaseResult, error) {
	var converted float64
	address := common.HexToAddress(account.Address)
	balance, err := ec.client.BalanceAt(ctx, address, nil)

	if err != nil {
		return nil, fmt.Errorf("failed to get balance for %s: %w", account.Address, err)
	}

	// Convert using currency package
	amount := new(big.Float).SetInt(balance)
	floatVal, _ := amount.Float64() // Extract just the float64 value
	logger.Infof("balance for %s: %s (%f)", account.Address, balance.String(), floatVal)

	if ec.currencyRegistry != nil {
		converted, err = ec.currencyRegistry.Convert(floatVal, "WEI", ec.unit.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to convert balance: %w", err)
		}
	} else {
		converted = floatVal
	}

	logger.Infof("balance for %s: %s (%f %s)", account.Address, balance.String(), converted, ec.unit.Symbol)

	return &BaseResult{
		NodeName: ec.nodeName,
		Account:  *account,
		Value:    converted,
		Health:   1.0,
	}, nil
}

// Close implements proper cleanup
func (ec *EVMCollector) Close() error {
	if ec.client != nil {
		ec.client.Close()
	}
	return nil
}
