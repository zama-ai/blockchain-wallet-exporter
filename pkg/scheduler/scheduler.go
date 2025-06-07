package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/collector"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/faucet"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

const (
	MAX_BUFFER_SIZE = 100
)

// RefundScheduler manages automatic account refunding based on balance thresholds
type RefundScheduler struct {
	config           *config.Schema
	faucetClient     faucet.Fauceter
	currencyRegistry *currency.Registry
	collectors       map[string]collector.IModuleCollector
	cron             *cron.Cron

	// Pre-calculated buffer size for event channel
	eventBufferSize int

	running  bool
	mutex    sync.RWMutex
	stopChan chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

// RefundEvent represents a refund event for monitoring and logging
type RefundEvent struct {
	NodeName       string
	AccountName    string
	AccountAddress string
	CurrentBalance float64
	Threshold      float64
	TargetAmount   float64
	RefundAmount   float64
	AmountWei      float64
	SourceUnit     string
	Session        string
	Status         string
	ClaimStatus    string
	ClaimHash      string
	ClaimBlock     int64
	Confirmed      bool
	Success        bool
	Error          error
	Timestamp      time.Time
	Duration       time.Duration
}

// NewRefundScheduler creates a new refund scheduler
func NewRefundScheduler(cfg *config.Schema, currencyRegistry *currency.Registry, faucetClient faucet.Fauceter) (*RefundScheduler, error) {
	if cfg.Global.AutoRefund == nil || !cfg.Global.AutoRefund.Enabled {
		return nil, fmt.Errorf("auto-refund is not enabled in configuration")
	}

	if cfg.Global.AutoRefund.FaucetURL == "" {
		return nil, fmt.Errorf("faucet URL is required for auto-refund")
	}

	// Note: Faucet unit validation removed - faucet now only accepts wei amounts
	// All currency conversion is handled in the scheduler

	// Create collectors for each node
	collectors := make(map[string]collector.IModuleCollector)
	for _, node := range cfg.Nodes {
		promCollector, err := collector.NewCollector(*node, currencyRegistry)
		if err != nil {
			return nil, fmt.Errorf("failed to create collector for node %s: %w", node.Name, err)
		}
		moduleCollector, ok := promCollector.(collector.IModuleCollector)
		if !ok {
			return nil, fmt.Errorf("collector for node %s does not implement IModuleCollector", node.Name)
		}
		collectors[node.Name] = moduleCollector
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Calculate buffer size based on refund-enabled accounts
	refundAccountCount := 0
	for _, node := range cfg.Nodes {
		for _, account := range node.Accounts {
			if account.RefundThreshold != nil && account.RefundTarget != nil {
				refundAccountCount++
			}
		}
	}

	// Set buffer size with sensible minimum
	eventBufferSize := max(refundAccountCount, MAX_BUFFER_SIZE)

	logger.Infof("Auto-refund scheduler configured for %d accounts with event buffer size %d",
		refundAccountCount, eventBufferSize)

	scheduler := &RefundScheduler{
		config:           cfg,
		faucetClient:     faucetClient,
		currencyRegistry: currencyRegistry,
		collectors:       collectors,
		cron:             cron.New(cron.WithSeconds()),
		eventBufferSize:  eventBufferSize, // Store pre-calculated size
		stopChan:         make(chan struct{}),
		ctx:              ctx,
		cancel:           cancel,
	}

	return scheduler, nil
}

// Start starts the refund scheduler
func (rs *RefundScheduler) Start() error {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()

	if rs.running {
		return fmt.Errorf("scheduler is already running")
	}

	logger.Infof("Starting auto-refund scheduler with schedule: %s", rs.config.Global.AutoRefund.Schedule)

	// Add cron job
	_, err := rs.cron.AddFunc(rs.config.Global.AutoRefund.Schedule, func() {
		rs.executeRefundCheck()
	})
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	rs.cron.Start()
	rs.running = true

	logger.Infof("Auto-refund scheduler started successfully")
	return nil
}

// Stop stops the refund scheduler
func (rs *RefundScheduler) Stop() error {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()

	if !rs.running {
		return nil
	}

	logger.Infof("Stopping auto-refund scheduler...")

	// Stop cron scheduler
	ctx := rs.cron.Stop()
	<-ctx.Done()

	// Cancel context and close stop channel
	rs.cancel()
	close(rs.stopChan)

	// Close all collectors
	for nodeName, collector := range rs.collectors {
		if err := collector.Close(); err != nil {
			logger.Errorf("Failed to close collector for node %s: %v", nodeName, err)
		}
	}

	rs.running = false
	logger.Infof("Auto-refund scheduler stopped")
	return nil
}

// executeRefundCheck performs the main refund check logic
func (rs *RefundScheduler) executeRefundCheck() {
	logger.Infof("Starting auto-refund check cycle")
	startTime := time.Now()

	events := rs.processAccounts()

	// Log summary
	successCount := 0
	errorCount := 0
	totalRefunded := 0.0

	for _, event := range events {
		if event.Success {
			successCount++
			totalRefunded += event.RefundAmount
		} else {
			errorCount++
		}
	}

	duration := time.Since(startTime)
	logger.Infof("Auto-refund check completed in %v: %d successful, %d errors, %.0f wei total refunded",
		duration, successCount, errorCount, totalRefunded)
}

// processAccounts checks all accounts and refunds those below threshold
func (rs *RefundScheduler) processAccounts() []*RefundEvent {
	var events []*RefundEvent
	var wg sync.WaitGroup

	// Use pre-calculated buffer size - no more deadlocks!
	eventChan := make(chan *RefundEvent, rs.eventBufferSize)

	// Process each node
	for _, node := range rs.config.Nodes {
		metricsCollector, exists := rs.collectors[node.Name]
		if !exists {
			logger.Errorf("No collector found for node %s", node.Name)
			continue
		}

		// Process each account in the node
		for _, account := range node.Accounts {
			if account.RefundThreshold == nil || account.RefundTarget == nil {
				logger.Debugf("Skipping account %s - no refund configuration", account.Name)
				continue
			}

			wg.Add(1)
			go func(n *config.Node, acc *config.Account, col collector.IModuleCollector) {
				defer wg.Done()
				event := rs.processAccount(n, acc, col)
				if event != nil {
					eventChan <- event // Now guaranteed to never block!
				}
			}(node, account, metricsCollector)
		}
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(eventChan)
	}()

	// Collect all events
	for event := range eventChan {
		events = append(events, event)
	}

	return events
}

// processAccount processes a single account for refunding
func (rs *RefundScheduler) processAccount(node *config.Node, account *config.Account, collector collector.IModuleCollector) *RefundEvent {
	ctx, cancel := context.WithTimeout(rs.ctx, 30*time.Second)
	defer cancel()

	startTime := time.Now()
	event := &RefundEvent{
		NodeName:       node.Name,
		AccountName:    account.Name,
		AccountAddress: account.Address,
		Timestamp:      startTime,
	}

	// Get current balance (returned in metricsUnit, expecting wei based on configuration)
	result, err := collector.CollectAccountBalance(ctx, account)
	if err != nil {
		event.Error = fmt.Errorf("failed to get balance: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("Failed to get balance for account %s: %v", account.Name, err)
		return event
	}

	if result.Health <= 0 {
		event.Error = fmt.Errorf("account health check failed")
		event.Duration = time.Since(startTime)
		logger.Errorf("Health check failed for account %s", account.Name)
		return event
	}

	logger.Debugf("Collector returned balance for %s: %.0f (raw value)", account.Name, result.Value)
	event.CurrentBalance = result.Value

	// Determine the unit that the collector returned (metricsUnit)
	collectorUnit := "eth" // Default fallback
	if node.MetricsUnit != nil && node.MetricsUnit.Name != "" {
		collectorUnit = node.MetricsUnit.Name
	} else if node.Unit != nil && node.Unit.Name != "" {
		collectorUnit = node.Unit.Name
	}

	logger.Debugf("Collector unit determined as: %s", collectorUnit)

	// Convert threshold and target from config unit to wei for faucet
	thresholdWei, err := rs.convertToWei(*account.RefundThreshold, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert threshold to wei: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("Failed to convert threshold to wei for account %s: %v", account.Name, err)
		return event
	}
	logger.Debugf("Threshold conversion: %.6f %s -> %.0f wei", *account.RefundThreshold, collectorUnit, thresholdWei)

	targetWei, err := rs.convertToWei(*account.RefundTarget, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert target to wei: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("Failed to convert target to wei for account %s: %v", account.Name, err)
		return event
	}
	logger.Debugf("Target conversion: %.6f %s -> %.0f wei", *account.RefundTarget, collectorUnit, targetWei)

	// Convert collector balance to wei for comparison
	currentBalanceWei, err := rs.convertToWei(result.Value, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert current balance to wei: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("Failed to convert current balance to wei for account %s: %v", account.Name, err)
		return event
	}
	logger.Debugf("Balance conversion: %.0f %s -> %.0f wei", result.Value, collectorUnit, currentBalanceWei)

	// Update event with wei amount
	event.CurrentBalance = currentBalanceWei

	// Compare in wei
	if currentBalanceWei >= thresholdWei {
		logger.Debugf("Account %s balance %.6f %s (%.0f wei) is above threshold %.6f %s (%.0f wei), no refund needed",
			account.Name, result.Value, collectorUnit, currentBalanceWei, *account.RefundThreshold, collectorUnit, thresholdWei)
		return nil
	}

	// Calculate refund amount in wei
	refundAmountWei := targetWei - currentBalanceWei
	if refundAmountWei <= 0 {
		logger.Warnf("Invalid refund amount %.0f wei for account %s", refundAmountWei, account.Name)
		return nil
	}
	logger.Debugf("Refund calculation: %.0f (target) - %.0f (current) = %.0f wei", targetWei, currentBalanceWei, refundAmountWei)

	// Convert values to human-readable units for display purposes only
	currentBalanceDisplay, err := rs.convertFromWei(currentBalanceWei, collectorUnit)
	if err != nil {
		logger.Warnf("Failed to convert current balance for display: %v", err)
		currentBalanceDisplay = currentBalanceWei // fallback to wei
	}

	refundAmountDisplay, err := rs.convertFromWei(refundAmountWei, collectorUnit)
	if err != nil {
		logger.Warnf("Failed to convert refund amount for display: %v", err)
		refundAmountDisplay = refundAmountWei // fallback to wei
	}

	// Update event with refund details
	event.RefundAmount = refundAmountWei
	event.AmountWei = refundAmountWei
	event.SourceUnit = "wei"
	event.Threshold = *account.RefundThreshold
	event.TargetAmount = *account.RefundTarget

	logger.Infof("Account %s balance %.6f %s (%.0f wei) is below threshold %.6f %s, refunding %.6f %s (%.0f wei) to reach target %.6f %s",
		account.Name, currentBalanceDisplay, collectorUnit, currentBalanceWei, *account.RefundThreshold, collectorUnit,
		refundAmountDisplay, collectorUnit, refundAmountWei, *account.RefundTarget, collectorUnit)

	// Call faucet directly with wei amount (specify unit as wei to avoid conversion)
	logger.Debugf("Calling faucet with amount: %.0f wei for account %s", refundAmountWei, account.Address)
	faucetResult, err := rs.faucetClient.FundAccountWeiWithRetry(ctx, account.Address, refundAmountWei, 2)
	if err != nil {
		event.Error = fmt.Errorf("failed to fund account: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("Failed to fund account %s: %v", account.Name, err)
		return event
	}

	// Update event with faucet result
	event.Session = faucetResult.Session
	event.Status = faucetResult.Status
	event.ClaimStatus = faucetResult.ClaimStatus
	event.ClaimHash = faucetResult.ClaimHash
	event.ClaimBlock = faucetResult.ClaimBlock
	event.Confirmed = faucetResult.Confirmed
	event.Success = faucetResult.Success
	event.Duration = time.Since(startTime)

	if faucetResult.Success {
		logMsg := fmt.Sprintf("Successfully refunded account %s with %.6f %s (%.0f wei), session: %s, status: %s, claim status: %s",
			account.Name, refundAmountDisplay, collectorUnit, event.AmountWei, event.Session, event.Status, event.ClaimStatus)

		if event.Confirmed && event.ClaimHash != "" {
			logMsg += fmt.Sprintf(", confirmed with tx: %s at block %d", event.ClaimHash, event.ClaimBlock)
		}

		logger.Infof(logMsg)
	} else {
		event.Error = fmt.Errorf("faucet funding failed: %v", faucetResult.Error)
		logger.Errorf("Faucet funding failed for account %s: %v", account.Name, faucetResult.Error)
	}

	return event
}

// IsRunning returns whether the scheduler is currently running
func (rs *RefundScheduler) IsRunning() bool {
	rs.mutex.RLock()
	defer rs.mutex.RUnlock()
	return rs.running
}

// GetNextRun returns the next scheduled run time
func (rs *RefundScheduler) GetNextRun() time.Time {
	if !rs.running {
		return time.Time{}
	}
	entries := rs.cron.Entries()
	if len(entries) > 0 {
		return entries[0].Next
	}
	return time.Time{}
}

// convertToWei converts amount from sourceUnit to wei
func (rs *RefundScheduler) convertToWei(amount float64, sourceUnit string) (float64, error) {
	if sourceUnit == "wei" {
		return amount, nil
	}
	// Use the scheduler's currency registry
	return rs.currencyRegistry.Convert(amount, sourceUnit, "wei")
}

// convertFromWei converts amount from wei to targetUnit
func (rs *RefundScheduler) convertFromWei(amountWei float64, targetUnit string) (float64, error) {
	if targetUnit == "wei" {
		return amountWei, nil
	}
	return rs.currencyRegistry.Convert(amountWei, "wei", targetUnit)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
