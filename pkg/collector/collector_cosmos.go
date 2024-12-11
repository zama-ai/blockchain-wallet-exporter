package collector

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// CosmosCollector implements the IModuleCollector interface for Cosmos chains
type CosmosCollector struct {
	nodeName string
	conn     *grpc.ClientConn
	client   banktypes.QueryClient
	labels   map[string]string
	unit     *currency.Unit
	currency *currency.Registry
}

// CosmosCollectorOption defines functional options for CosmosCollector
type CosmosCollectorOption func(*CosmosCollector)

func WithCosmosLabels(labels map[string]string) CosmosCollectorOption {
	return func(cc *CosmosCollector) {
		cc.labels = labels
	}
}

func NewCosmosCollector(node config.Node, currency *currency.Registry, opts ...CosmosCollectorOption) (prometheus.Collector, error) {
	// Create a connection to the gRPC server using NewClientConn
	grpcAddr := strings.TrimPrefix(node.GrpcAddr, "grpc://")
	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("tcp", addr, 10*time.Second)
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to connect to cosmos node: %w", err)
	}

	cosmosCollector := &CosmosCollector{
		nodeName: node.Name,
		conn:     conn,
		client:   banktypes.NewQueryClient(conn),
		unit:     node.Unit,
		labels:   node.Labels,
		currency: currency,
	}

	// Apply options if needed for additional labels
	for _, opt := range opts {
		opt(cosmosCollector)
	}

	return NewBaseCollector(
		node,
		cosmosCollector,
		WithCollectorTimeout(10*time.Second),
	), nil
}

func WithCosmosCurrencyRegistry(registry *currency.Registry) CosmosCollectorOption {
	return func(cc *CosmosCollector) {
		cc.currency = registry
	}
}

// CollectAccountBalance implements IModuleCollector interface
func (cc *CosmosCollector) CollectAccountBalance(ctx context.Context, account *config.Account) (*BaseResult, error) {
	logger.Infof("collecting balance for %s", account.Address)

	req := &banktypes.QueryBalanceRequest{
		Address: account.Address,
		Denom:   cc.unit.Name,
	}

	balance, err := cc.client.Balance(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance for %s: %w", account.Address, err)
	}

	// Convert to big.Float first
	amount := new(big.Float).SetInt(balance.Balance.Amount.BigInt())

	// Check if amount exceeds float64 range
	if amount.IsInf() {
		return nil, fmt.Errorf("balance for %s exceeds float64 range", account.Address)
	}

	var totalValue float64
	totalValue, _ = amount.Float64()

	logger.Debugf("balance for %s: %s (%f %s)", account.Address, balance.String(), totalValue, cc.unit.Symbol)

	return &BaseResult{
		NodeName: cc.nodeName,
		Account:  *account,
		Value:    totalValue,
		Health:   1.0,
	}, nil
}

// Close implements proper cleanup
func (cc *CosmosCollector) Close() error {
	if cc.conn != nil {
		return cc.conn.Close()
	}
	return nil
}
