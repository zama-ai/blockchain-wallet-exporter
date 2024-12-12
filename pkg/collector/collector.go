package collector

import (
	"context"
	"sync"
	"time"

	"github.com/carlmjohnson/flowmatic"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

const (
	DefaultMaxConcurrency = 10
)

type Module string

const (
	Cosmos Module = "cosmos"
	EVM    Module = "evm"
)

var ModuleNames = map[string]Module{
	"cosmos": Cosmos,
	"evm":    EVM,
}

type BaseResult struct {
	NodeName string
	Account  config.Account
	Value    float64
	Health   float64
}

// BaseCollector provides enhanced common functionality for all collectors
type BaseCollector struct {
	nodeName     string
	module       Module
	metrics      *prometheus.GaugeVec
	health       *prometheus.GaugeVec
	processor    IModuleCollector
	timeout      time.Duration
	unit         *currency.Unit
	accounts     []*config.Account
	labels       map[string]string
	collectMutex sync.Mutex
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

func NewBaseCollector(node *config.Node, processor IModuleCollector, opts ...CollectorOption) prometheus.Collector {
	var (
		constLabels prometheus.Labels
	)

	if node.Labels != nil {
		constLabels = node.Labels
	}

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
	}

	// Apply options
	for _, opt := range opts {
		opt(collector)
	}

	return collector
}

// collectMetrics handles the concurrent collection of metrics with retry logic
func (c *BaseCollector) collectMetrics() []*BaseResult {
	results := make([]*BaseResult, 0)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	// Create a buffered channel to collect results
	resultsChan := make(chan *BaseResult, len(c.accounts))

	err := flowmatic.Each(DefaultMaxConcurrency, c.accounts, func(account *config.Account) error {
		logger.Debugf("collecting metrics for account: %s", account.Address)

		result, err := c.processor.CollectAccountBalance(ctx, account)
		if err != nil {
			logger.Errorf("error collecting metrics for account %s: %v", account.Address, err)
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

	return results
}

// Implement prometheus.Collector interface
func (c *BaseCollector) Describe(ch chan<- *prometheus.Desc) {
	c.metrics.Describe(ch)
	c.health.Describe(ch)
}

func (c *BaseCollector) Collect(ch chan<- prometheus.Metric) {
	c.collectMutex.Lock()
	defer c.collectMutex.Unlock()
	logger.Infof("collecting metrics from %s", c.nodeName)

	metrics := c.collectMetrics()

	for _, result := range metrics {
		logger.Debugf("%s: %f", string(c.module), result.Health)

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
}

func (c *BaseCollector) Name() string {
	return string(c.module)
}

// Close implements proper cleanup
func (c *BaseCollector) Close() error {
	return c.processor.Close()
}
