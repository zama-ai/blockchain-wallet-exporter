package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/scheduler"
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
	config, err := config.ReadConfigWithError(file)
	if err != nil {
		panic(fmt.Errorf("failed to read config: %v", err))
	}

	// init logger
	level, err := zapcore.ParseLevel(config.Global.LogLevel)
	if err != nil {
		panic(fmt.Errorf("failed to parse log level: %v", err))
	}
	err = logger.InitLogger(logger.WithLevel(level))
	if err != nil {
		panic(fmt.Errorf("failed to init logger: %v", err))
	}

	// Initialize currency registry
	currencyRegistry := currency.NewDefaultRegistry()

	// Initialize prometheus registry
	promRegistry := prometheus.NewRegistry()

	// Initialize auto-refund scheduler if enabled
	var refundScheduler *scheduler.RefundScheduler
	if config.Global.AutoRefund != nil && config.Global.AutoRefund.Enabled {
		logger.Infof("Auto-refund is enabled, initializing scheduler...")
		refundScheduler, err = scheduler.NewRefundScheduler(config, currencyRegistry)
		if err != nil {
			logger.Fatalf("Failed to create refund scheduler: %v", err)
		}

		// Start scheduler in a separate goroutine
		go func() {
			if err := refundScheduler.Start(); err != nil {
				logger.Fatalf("Failed to start refund scheduler: %v", err)
			}
		}()

		logger.Infof("Auto-refund scheduler started with schedule: %s", config.Global.AutoRefund.Schedule)
	} else {
		logger.Infof("Auto-refund is disabled")
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

	// Stop scheduler first
	if refundScheduler != nil {
		logger.Infof("Stopping auto-refund scheduler...")
		if err := refundScheduler.Stop(); err != nil {
			logger.Errorf("Failed to stop refund scheduler: %v", err)
		}
	}

	// Stop server
	server.Stop()
	logger.Infof("Shutdown complete")
}
