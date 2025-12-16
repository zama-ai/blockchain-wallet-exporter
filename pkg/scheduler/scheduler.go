package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/carlmjohnson/flowmatic"
	"github.com/robfig/cron/v3"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/collector"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/faucet"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

const (
	MAX_WORKER_GOROUTINES = 20 // Maximum concurrent workers for processing accounts
)

// RefundScheduler manages automatic account refunding based on balance thresholds
type RefundScheduler struct {
	node             *config.Node
	faucetClient     faucet.Fauceter
	currencyRegistry *currency.Registry
	collector        collector.IModuleCollector
	cron             *cron.Cron

	running bool
	mutex   sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
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
	AmountBaseUnit float64
	BaseUnit       string
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

// logPrefix returns a consistent log prefix for this scheduler
func (rs *RefundScheduler) logPrefix() string {
	return fmt.Sprintf("[scheduler %s]", rs.node.Name)
}

// NewNodeRefundScheduler creates a refund scheduler for a specific node
func NewNodeRefundScheduler(node *config.Node, currencyRegistry *currency.Registry, faucetClient faucet.Fauceter, nodeCollector collector.IModuleCollector) (*RefundScheduler, error) {
	// Validate that the node has autoRefund properly configured
	if !node.IsAutoRefundEnabled() {
		return nil, fmt.Errorf("auto-refund is not enabled or properly configured for node %s", node.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Count refund-enabled accounts for this node
	refundAccountCount := 0
	for _, account := range node.Accounts {
		if account.RefundThreshold != nil && account.RefundTarget != nil {
			refundAccountCount++
		}
	}

	logger.Debugf("[scheduler %s] Node-specific scheduler configured for %d accounts with max %d concurrent workers",
		node.Name, refundAccountCount, MAX_WORKER_GOROUTINES)

	scheduler := &RefundScheduler{
		node:             node,
		faucetClient:     faucetClient,
		currencyRegistry: currencyRegistry,
		collector:        nodeCollector,
		cron:             cron.New(cron.WithSeconds()),
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

	logger.Infof("%s Starting auto-refund scheduler with schedule: %s", rs.logPrefix(), rs.node.AutoRefund.Schedule)

	// Add cron job
	_, err := rs.cron.AddFunc(rs.node.AutoRefund.Schedule, func() {
		rs.executeRefundCheck()
	})
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	rs.cron.Start()
	rs.running = true

	logger.Infof("%s Auto-refund scheduler started successfully", rs.logPrefix())
	return nil
}

// Stop stops the refund scheduler
func (rs *RefundScheduler) Stop() error {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()

	if !rs.running {
		return nil
	}

	logger.Infof("%s Stopping auto-refund scheduler...", rs.logPrefix())

	// Stop cron scheduler
	ctx := rs.cron.Stop()
	<-ctx.Done()

	// Cancel context
	rs.cancel()

	// Close collector
	if err := rs.collector.Close(); err != nil {
		logger.Errorf("%s Failed to close collector: %v", rs.logPrefix(), err)
	}

	rs.running = false
	logger.Infof("%s Auto-refund scheduler stopped", rs.logPrefix())
	return nil
}

// executeRefundCheck performs the main refund check logic
func (rs *RefundScheduler) executeRefundCheck() {
	logger.Infof("%s Starting auto-refund check cycle", rs.logPrefix())
	startTime := time.Now()

	events := rs.processAccounts()

	// Log summary
	successCount := 0
	errorCount := 0
	totalRefunded := 0.0
	baseUnit := rs.baseUnitName()

	for _, event := range events {
		if event.Success {
			successCount++
			totalRefunded += event.RefundAmount
		} else {
			errorCount++
		}
	}

	duration := time.Since(startTime)
	logger.Infof("%s Auto-refund check completed in %v: %d successful, %d errors, %.0f %s total refunded",
		rs.logPrefix(), duration, successCount, errorCount, totalRefunded, baseUnit)
}

// processAccounts checks all accounts and refunds those below threshold
func (rs *RefundScheduler) processAccounts() []*RefundEvent {
	// Filter accounts that have refund configuration
	var refundAccounts []*config.Account
	for _, account := range rs.node.Accounts {
		if account.RefundThreshold == nil || account.RefundTarget == nil {
			logger.Debugf("%s Skipping account %s - no refund configuration", rs.logPrefix(), account.Name)
			continue
		}
		refundAccounts = append(refundAccounts, account)
	}

	if len(refundAccounts) == 0 {
		logger.Debugf("%s No accounts configured for refund", rs.logPrefix())
		return nil
	}

	// Use channel to collect results
	eventChan := make(chan *RefundEvent, len(refundAccounts))

	logger.Debugf("%s Processing %d accounts with max %d concurrent workers",
		rs.logPrefix(), len(refundAccounts), MAX_WORKER_GOROUTINES)

	err := flowmatic.Each(MAX_WORKER_GOROUTINES, refundAccounts, func(account *config.Account) error {
		event := rs.processAccount(account)
		if event != nil {
			eventChan <- event
		}
		return nil
	})

	// Close channel to signal no more events
	close(eventChan)

	// Collect all events from channel
	var events []*RefundEvent
	for event := range eventChan {
		events = append(events, event)
	}

	if err != nil {
		logger.Errorf("%s Error during account processing: %v", rs.logPrefix(), err)
	}

	return events
}

// processAccount processes a single account for refunding
func (rs *RefundScheduler) processAccount(account *config.Account) *RefundEvent {
	// Use a longer timeout to accommodate faucet confirmation (default 5 minutes)
	// Add extra buffer for balance check and retries
	ctx, cancel := context.WithTimeout(rs.ctx, 10*time.Minute)
	defer cancel()

	startTime := time.Now()
	event := &RefundEvent{
		NodeName:       rs.node.Name,
		AccountName:    account.Name,
		AccountAddress: account.Address,
		Timestamp:      startTime,
	}

	// Get current balance (returned in metricsUnit after conversion from blockchain's native unit)
	// For EVM: blockchain returns wei, collector converts to metricsUnit (e.g., eth for human readability)
	result, err := rs.collector.CollectAccountBalance(ctx, account)
	if err != nil {
		event.Error = fmt.Errorf("failed to get balance: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to get balance for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}

	if result.Health <= 0 {
		event.Error = fmt.Errorf("account health check failed")
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Health check failed for account %s", rs.logPrefix(), account.Name)
		return event
	}

	logger.Debugf("%s Collector returned balance for %s: %.6f (in metricsUnit)", rs.logPrefix(), account.Name, result.Value)
	event.CurrentBalance = result.Value

	// Determine the unit that the collector returned (should be metricsUnit after conversion)
	collectorUnit := "eth" // Default fallback
	if rs.node.MetricsUnit != nil && rs.node.MetricsUnit.Name != "" {
		collectorUnit = rs.node.MetricsUnit.Name
	} else if rs.node.Unit != nil && rs.node.Unit.Name != "" {
		collectorUnit = rs.node.Unit.Name
	}

	logger.Debugf("%s Collector unit determined as: %s", rs.logPrefix(), collectorUnit)

	baseUnitName := rs.baseUnitName()
	logger.Debugf("%s Base unit determined as: %s", rs.logPrefix(), baseUnitName)

	// Convert threshold and target from config unit to the base unit for faucet
	thresholdBase, err := rs.convertToBaseUnit(*account.RefundThreshold, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert threshold to base unit: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to convert threshold to base unit for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}
	logger.Debugf("%s Threshold conversion: %.6f %s -> %.0f %s", rs.logPrefix(), *account.RefundThreshold, collectorUnit, thresholdBase, baseUnitName)

	targetBase, err := rs.convertToBaseUnit(*account.RefundTarget, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert target to base unit: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to convert target to base unit for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}
	logger.Debugf("%s Target conversion: %.6f %s -> %.0f %s", rs.logPrefix(), *account.RefundTarget, collectorUnit, targetBase, baseUnitName)

	// Convert collector balance to base unit for comparison
	currentBalanceBase, err := rs.convertToBaseUnit(result.Value, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert current balance to base unit: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to convert current balance to base unit for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}
	logger.Debugf("%s Balance conversion: %.6f %s -> %.0f %s", rs.logPrefix(), result.Value, collectorUnit, currentBalanceBase, baseUnitName)

	// Update event with base amount
	event.CurrentBalance = currentBalanceBase

	// Compare in base unit
	if currentBalanceBase >= thresholdBase {
		logger.Debugf("%s Account %s balance %.6f %s (%.0f %s) is above threshold %.6f %s (%.0f %s), no refund needed",
			rs.logPrefix(), account.Name, result.Value, collectorUnit, currentBalanceBase, baseUnitName, *account.RefundThreshold, collectorUnit, thresholdBase, baseUnitName)
		return nil
	}

	// Calculate refund amount in base units
	refundAmountBase := targetBase - currentBalanceBase
	if refundAmountBase <= 0 {
		logger.Warnf("%s Invalid refund amount %.0f %s for account %s", rs.logPrefix(), refundAmountBase, baseUnitName, account.Name)
		return nil
	}
	logger.Debugf("%s Refund calculation: %.0f (target) - %.0f (current) = %.0f %s", rs.logPrefix(), targetBase, currentBalanceBase, refundAmountBase, baseUnitName)

	// Convert values to human-readable units for display purposes only
	currentBalanceDisplay, err := rs.convertFromBaseUnit(currentBalanceBase, collectorUnit)
	if err != nil {
		logger.Warnf("%s Failed to convert current balance for display: %v", rs.logPrefix(), err)
		currentBalanceDisplay = currentBalanceBase // fallback to base unit
	}

	refundAmountDisplay, err := rs.convertFromBaseUnit(refundAmountBase, collectorUnit)
	if err != nil {
		logger.Warnf("%s Failed to convert refund amount for display: %v", rs.logPrefix(), err)
		refundAmountDisplay = refundAmountBase // fallback to base unit
	}

	// Update event with refund details
	event.RefundAmount = refundAmountBase
	event.AmountBaseUnit = refundAmountBase
	event.BaseUnit = baseUnitName
	event.Threshold = *account.RefundThreshold
	event.TargetAmount = *account.RefundTarget

	logger.Infof("%s Account %s balance %.6f %s (%.0f %s) is below threshold %.6f %s, refunding %.6f %s (%.0f %s) to reach target %.6f %s",
		rs.logPrefix(), account.Name, currentBalanceDisplay, collectorUnit, currentBalanceBase, baseUnitName, *account.RefundThreshold, collectorUnit,
		refundAmountDisplay, collectorUnit, refundAmountBase, baseUnitName, *account.RefundTarget, collectorUnit)

	// Call faucet directly with base unit amount
	logger.Debugf("%s Calling faucet with amount: %.0f %s for account %s", rs.logPrefix(), refundAmountBase, baseUnitName, account.Address)
	faucetResult, err := rs.faucetClient.FundAccountWeiWithRetry(ctx, account.Address, refundAmountBase, 2)
	if err != nil {
		event.Error = fmt.Errorf("failed to fund account: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to fund account %s: %v", rs.logPrefix(), account.Name, err)
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
		var logMsg string
		if event.Confirmed && event.ClaimHash != "" {
			// Transaction has been confirmed on-chain
			logMsg = fmt.Sprintf("%s Successfully refunded account %s with %.6f %s (%.0f %s), session: %s, confirmed with tx: %s at block %d",
				rs.logPrefix(), account.Name, refundAmountDisplay, collectorUnit, event.AmountBaseUnit, baseUnitName, event.Session, event.ClaimHash, event.ClaimBlock)
		} else {
			// Transaction is queued but not yet confirmed
			logMsg = fmt.Sprintf("%s Queued refund for account %s with %.6f %s (%.0f %s), session: %s, status: %s, claim status: %s",
				rs.logPrefix(), account.Name, refundAmountDisplay, collectorUnit, event.AmountBaseUnit, baseUnitName, event.Session, event.Status, event.ClaimStatus)
		}

		logger.Infof(logMsg)
	} else {
		event.Error = fmt.Errorf("faucet funding failed: %v", faucetResult.Error)
		logger.Errorf("%s Faucet funding failed for account %s: %v", rs.logPrefix(), account.Name, faucetResult.Error)
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

func (rs *RefundScheduler) baseUnitName() string {
	if rs.node != nil && rs.node.Unit != nil && rs.node.Unit.Name != "" {
		return rs.node.Unit.Name
	}
	return "wei"
}

// convertToBaseUnit converts amount from sourceUnit to the node's base unit
func (rs *RefundScheduler) convertToBaseUnit(amount float64, sourceUnit string) (float64, error) {
	baseUnit := rs.baseUnitName()
	if sourceUnit == baseUnit {
		return amount, nil
	}
	return rs.currencyRegistry.Convert(amount, sourceUnit, baseUnit)
}

// convertFromBaseUnit converts amount from the node's base unit to the targetUnit
func (rs *RefundScheduler) convertFromBaseUnit(amount float64, targetUnit string) (float64, error) {
	baseUnit := rs.baseUnitName()
	if targetUnit == baseUnit {
		return amount, nil
	}
	return rs.currencyRegistry.Convert(amount, baseUnit, targetUnit)
}
