package collector

import (
	"context"
	"fmt"
	"net"
	"strconv"
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

func NewCosmosCollector(node config.Node, opts ...CosmosCollectorOption) (prometheus.Collector, error) {
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
	req := &banktypes.QueryAllBalancesRequest{
		Address: account.Address,
	}

	balance, err := cc.client.AllBalances(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance for %s: %w", account.Address, err)
	}

	// Sum up all balances (assuming we want total value in the native token)
	var totalValue float64
	for _, coin := range balance.Balances {
		// Convert amount to float64
		logger.Infof("coin: %s", coin.Amount)
		amount, err := strconv.ParseFloat(coin.Amount.String(), 64)
		if err != nil {
			logger.Warnf("failed to convert amount %s to float64", coin.Amount)
			return nil, err
		}
		totalValue += amount
	}

	if cc.currency != nil {
		totalValue, err = cc.currency.Convert(totalValue, cc.unit.Name, cc.unit.Name)
		if err != nil {
			logger.Warnf("failed to convert amount %s to %s", totalValue, cc.unit.Name)
			return nil, err
		}
	}

	logger.Infof("balance for %s: %s (%f %s)", account.Address, balance.String(), totalValue, cc.unit.Symbol)

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
