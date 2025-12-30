package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/collector"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/erc20"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/scheduler"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/validation"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/version"

	httpfiber "github.com/zama-ai/blockchain-wallet-exporter/pkg/server/http"
	"go.uber.org/zap/zapcore"
)

var (
	cfgPath     = flag.String("config", "config.yaml", "path to the config file")
	showVersion = flag.Bool("version", false, "print version information")
)

func main() {
	flag.Parse()

	if *showVersion {
		versionInfo := version.GetVersion()
		versionJSON, _ := json.Marshal(versionInfo)
		fmt.Println(string(versionJSON))
		return
	}

	file, err := os.Open(*cfgPath)
	if err != nil {
		panic(fmt.Errorf("failed to open config file: %v", err))
	}
	defer file.Close()

	config, err := config.ReadConfigWithError(file)
	if err != nil {
		panic(fmt.Errorf("failed to read config: %v", err))
	}

	// init logger
	level, err := zapcore.ParseLevel(config.Global.LogLevel)
	if err != nil {
		panic(fmt.Errorf("failed to parse log level: %v", err))
	}
	err = logger.InitLogger(logger.WithLevel(level), logger.WithEncodeTime("timestamp", zapcore.ISO8601TimeEncoder))
	if err != nil {
		panic(fmt.Errorf("failed to init logger: %v", err))
	}

	// Validate configuration before any network operations
	// This catches config errors early without needing to connect to nodes
	configValidator := validation.NewConfigValidator()
	if err := configValidator.ValidateConfig(config); err != nil {
		logger.Fatalf("Configuration validation failed: %v", err)
	}
	logger.Infof("Configuration validated successfully")

	// Initialize currency registry
	currencyRegistry := currency.NewDefaultRegistry()

	// Register ERC20 token units upfront so collectors/schedulers can convert values
	// Use a timeout to prevent startup hangs if RPC endpoints are unresponsive
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize collectors and register them with prometheus registry
	promRegistry := prometheus.NewRegistry()
	autoRefundEnabled := false
	collectors := make([]prometheus.Collector, 0)
	for _, node := range config.Nodes {
		if node.IsERC20Module() {
			meta, err := erc20.ResolveUnits(ctx, currencyRegistry, node)
			if err != nil {
				logger.Fatalf("Failed to prepare ERC20 units for node %s: %v", node.Name, err)
			}
			logger.Infof("Loaded ERC20 metadata for node %s (symbol=%s, decimals=%d)", node.Name, meta.Symbol, meta.Decimals)
		}
		if node.IsAutoRefundEnabled() {
			autoRefundEnabled = true
		}
		collector, err := collector.NewCollector(*node, currencyRegistry)
		if err != nil {
			logger.Fatalf("Failed to create collector for node %s: %v", node.Name, err)
		}
		err = promRegistry.Register(collector)
		if err != nil {
			logger.Fatalf("Failed to register collector for node %s: %v", node.Name, err)
		}
		collectors = append(collectors, collector)
	}

	// Initialize auto-refund scheduler manager
	var schedulerManager *scheduler.SchedulerManager
	if autoRefundEnabled {
		logger.Infof("Auto-refund is enabled on one or more nodes, initializing scheduler manager...")
		schedulerManager, err = scheduler.NewSchedulerManager(config, currencyRegistry)
		if err != nil {
			logger.Fatalf("Failed to create scheduler manager: %v", err)
		}

		// Start scheduler manager in a separate goroutine
		go func() {
			if err := schedulerManager.Start(); err != nil {
				logger.Fatalf("Failed to start scheduler manager: %v", err)
			}
		}()

		logger.Infof("Auto-refund scheduler manager initialized")
	} else {
		logger.Infof("Auto-refund is disabled - no nodes have autoRefund configured")
	}

	// Bootstrap Server with both registries
	server := httpfiber.NewServer(config,
		httpfiber.WithRegistry(promRegistry),
		httpfiber.WithCurrencyRegistry(currencyRegistry))

	signalChain := make(chan os.Signal, 1)
	signal.Notify(signalChain, os.Interrupt)
	go func() {
		if err := server.Run(); err != nil {
			logger.Fatalf("failed to run server: %v", err)
		}
	}()
	<-signalChain

	// Graceful shutdown
	logger.Infof("Shutting down...")

	// Close collectors
	for _, col := range collectors {
		if closer, ok := col.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logger.Errorf("failed to close collector: %v", err)
			}
		}
	}

	// Stop scheduler manager
	if schedulerManager != nil {
		logger.Infof("Stopping auto-refund scheduler manager...")
		if err := schedulerManager.Stop(); err != nil {
			logger.Errorf("Failed to stop scheduler manager: %v", err)
		}
	}

	// Stop server
	server.Stop()
	logger.Infof("Shutdown complete")
}
