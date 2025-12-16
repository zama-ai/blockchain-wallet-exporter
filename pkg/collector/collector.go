package collector

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/carlmjohnson/flowmatic"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/version"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultMaxConcurrency = 10
)

type Module string

const (
	Cosmos Module = "cosmos"
	EVM    Module = "evm"
	ERC20  Module = "erc20"
)

var ModuleNames = map[string]Module{
	"cosmos": Cosmos,
	"evm":    EVM,
	"erc20":  ERC20,
}

type BaseResult struct {
	NodeName string
	Account  config.Account
	Value    float64
	Health   float64
}

// BaseCollector provides enhanced common functionality for all collectors
type BaseCollector struct {
	nodeName        string
	module          Module
	metrics         *prometheus.GaugeVec
	health          *prometheus.GaugeVec
	nodeUnreachable prometheus.Counter
	processor       IModuleCollector
	timeout         time.Duration
	unit            *currency.Unit
	accounts        []*config.Account
	labels          map[string]string
	collectMutex    sync.Mutex
}

type PrometheusCollector interface {
	prometheus.Collector
	IModuleCollector
}

// CollectorOption defines functional options for BaseCollector
type CollectorOption func(*BaseCollector)

// WithCollectorTimeout sets the timeout for collection operations
func WithCollectorTimeout(timeout time.Duration) CollectorOption {
	return func(c *BaseCollector) {
		c.timeout = timeout
	}
}

// IModuleCollector defines the simplified interface for specific blockchain module collectors
type IModuleCollector interface {
	// CollectAccountBalance collects balance for a single account
	CollectAccountBalance(ctx context.Context, account *config.Account) (*BaseResult, error)
	Close() error
}

var _ IModuleCollector = (*BaseCollector)(nil)

func NewBaseCollector(node *config.Node, processor IModuleCollector, opts ...CollectorOption) prometheus.Collector {
	var (
		constLabels prometheus.Labels
	)

	if node.Labels != nil {
		constLabels = node.Labels
	}

	// add exporter version to constLabels
	constLabels["exporter_version"] = version.Version

	// append nodename and module to constLabels in order to have a unique identifier for the metrics
	constLabels["node_name"] = node.Name
	constLabels["module"] = string(ModuleNames[node.Module])
	constLabelsHealth := constLabels

	constLabels["unit"] = node.Unit.Symbol
	if node.MetricsUnit != nil {
		constLabels["unit"] = node.MetricsUnit.Symbol
	}
	logger.Infof("constLabels: %v", constLabels)

	collector := &BaseCollector{
		nodeName:  node.Name,
		module:    ModuleNames[node.Module],
		processor: processor,
		timeout:   10 * time.Second, // Default timeout
		unit:      node.Unit,
		accounts:  node.Accounts,
		labels:    node.Labels,
		metrics: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:        "blockchain_wallet_balance",
				Help:        "Balance for blockchain wallets",
				ConstLabels: constLabels,
			},
			[]string{"address", "account_name"},
		),
		health: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name:        "blockchain_wallet_health",
				Help:        "Health for blockchain wallets",
				ConstLabels: constLabelsHealth,
			},
			[]string{"address", "account_name"},
		),
		nodeUnreachable: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name:        "blockchain_wallet_node_unreachable_total",
				Help:        "Number of scrapes where 50% or more accounts failed to retrieve balance",
				ConstLabels: constLabelsHealth,
			},
		),
	}

	// Apply options
	for _, opt := range opts {
		opt(collector)
	}

	return collector
}

// TODO: Uncomment this when we have a way to detect network errors
// IsNodeUnreachable checks if an error indicates the node is unreachable (network error)
// It excludes application-level errors like "balance not found" or conversion errors
func IsNodeUnreachable(err error) bool {
	if err == nil {
		return false
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check for net.Error (includes timeouts)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Check for gRPC status codes
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return true
		}
	}

	errStr := strings.ToLower(err.Error())

	// Check for common network error substrings
	networkErrorPatterns := []string{
		"connection refused",
		"no such host",
		"i/o timeout",
		"dial tcp",
		"no route to host",
		"network is unreachable",
		"connection reset",
		"broken pipe",
		"failed to connect",
	}
	for _, pattern := range networkErrorPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// collectMetrics handles the concurrent collection of metrics with retry logic
func (c *BaseCollector) collectMetrics() []*BaseResult {
	results := make([]*BaseResult, 0)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	// Track network failures (thread-safe counter)
	var networkFailureCount int32
	totalAccounts := len(c.accounts)

	// Create a buffered channel to collect results
	resultsChan := make(chan *BaseResult, len(c.accounts))

	err := flowmatic.Each(DefaultMaxConcurrency, c.accounts, func(account *config.Account) error {
		logger.Debugf("collecting metrics for account: %s", account.Address)

		result, err := c.processor.CollectAccountBalance(ctx, account)
		if err != nil {
			logger.Errorf("error collecting metrics for account %s: %v", account.Address, err)

			// TODO: Uncomment this when we have a way to detect network errors
			//if isNodeUnreachable(err) {
			//	atomic.AddInt32(&networkFailureCount, 1)
			//}

			atomic.AddInt32(&networkFailureCount, 1)

			result = &BaseResult{
				NodeName: c.nodeName,
				Account:  *account,
				Health:   0,
			}
		}

		resultsChan <- result
		return nil
	})

	if err != nil {
		logger.Errorf("error in collection process: %v", err)
	}

	// Close channel and collect results
	close(resultsChan)
	for result := range resultsChan {
		results = append(results, result)
	}

	// Check if 50% or more accounts failed with network errors
	if totalAccounts > 0 {
		threshold := (totalAccounts + 1) / 2
		if int(networkFailureCount) >= threshold {
			logger.Warnf("node %s is unreachable or degraded: %d/%d accounts failed with network errors (threshold: >= %d)",
				c.nodeName, networkFailureCount, totalAccounts, threshold)
			c.nodeUnreachable.Inc()
		}
	}

	return results
}

// Implement prometheus.Collector interface
func (c *BaseCollector) Describe(ch chan<- *prometheus.Desc) {
	c.metrics.Describe(ch)
	c.health.Describe(ch)
	c.nodeUnreachable.Describe(ch)
}

func (c *BaseCollector) Collect(ch chan<- prometheus.Metric) {
	c.collectMutex.Lock()
	defer c.collectMutex.Unlock()
	logger.Infof("collecting metrics from %s", c.nodeName)

	metrics := c.collectMetrics()

	for _, result := range metrics {
		labels := prometheus.Labels{
			"address":      result.Account.Address,
			"account_name": result.Account.Name,
		}

		c.health.With(labels).Set(result.Health)
		c.health.Collect(ch)
		c.health.Reset()

		logger.Debugf("Collecting metric with labels: %v", labels)
		if result.Health > 0 {
			c.metrics.With(labels).Set(result.Value)
			c.metrics.Collect(ch)
			c.metrics.Reset()
		}
	}

	c.nodeUnreachable.Collect(ch)
}

func (c *BaseCollector) Name() string {
	return string(c.module)
}

// Close implements proper cleanup
func (c *BaseCollector) Close() error {
	return c.processor.Close()
}

func (c *BaseCollector) CollectAccountBalance(ctx context.Context, account *config.Account) (*BaseResult, error) {
	return c.processor.CollectAccountBalance(ctx, account)
}
