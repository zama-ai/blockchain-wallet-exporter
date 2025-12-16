package scheduler

import (
	"context"
	"fmt"
	"strings"
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
	MAX_WORKER_GOROUTINES      = 20               // Maximum concurrent workers for processing accounts
	CYCLE_SAFETY_MARGIN        = 5 * time.Second  // Safety margin to prevent cycle overlap
	MAX_FAUCET_RETRIES         = 3                // Maximum number of retry attempts (total attempts = 1 + retries)
	PER_ATTEMPT_TIMEOUT        = 15 * time.Second // Timeout for each faucet funding attempt
	PROCESSING_OVERHEAD_BUFFER = 10 * time.Second // Buffer for processing overhead (balance checks, etc.)
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

	// Create cycle context with dynamic timeout based on config and schedule
	cycleTimeout := rs.cycleTimeout(startTime)
	cycleCtx, cancel := context.WithTimeout(rs.ctx, cycleTimeout)
	defer cancel()

	logger.Debugf("%s Cycle timeout set to %v", rs.logPrefix(), cycleTimeout)

	events := rs.processAccounts(cycleCtx)

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
func (rs *RefundScheduler) processAccounts(ctx context.Context) []*RefundEvent {
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
		event := rs.processAccount(ctx, account)
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
func (rs *RefundScheduler) processAccount(ctx context.Context, account *config.Account) *RefundEvent {
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

	// Call faucet with custom funding options
	// Use per-attempt timeout to ensure retries can complete within cycle timeout
	logger.Debugf("%s Calling faucet with amount: %.0f %s for account %s", rs.logPrefix(), refundAmountBase, baseUnitName, account.Address)

	opts := &faucet.FundingOptions{
		WaitForConfirmation: true,
		ConfirmationTimeout: PER_ATTEMPT_TIMEOUT,
		PollInterval:        0, // Let it be calculated adaptively
		MaxRetries:          MAX_FAUCET_RETRIES,
	}

	logCtx := &faucet.LoggingContext{NodeName: rs.node.Name}
	faucetResult, err := rs.faucetClient.FundAccountWeiWithRetriesAndOptionsAndContext(ctx, account.Address, refundAmountBase, opts, logCtx)
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
		// With WaitForConfirmation=true (default), Success implies Confirmed
		if event.Confirmed && event.ClaimHash != "" {
			// Transaction has been confirmed on-chain
			logger.Infof("%s Successfully refunded account %s with %.6f %s (%.0f %s), session: %s, confirmed with tx: %s at block %d",
				rs.logPrefix(), account.Name, refundAmountDisplay, collectorUnit, event.AmountBaseUnit, baseUnitName, event.Session, event.ClaimHash, event.ClaimBlock)
		} else {
			// This should only happen if WaitForConfirmation=false (non-default)
			logger.Infof("%s Queued refund for account %s with %.6f %s (%.0f %s), session: %s, status: %s, claim status: %s (not waiting for confirmation)",
				rs.logPrefix(), account.Name, refundAmountDisplay, collectorUnit, event.AmountBaseUnit, baseUnitName, event.Session, event.Status, event.ClaimStatus)
		}
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
	rs.mutex.RLock()
	defer rs.mutex.RUnlock()

	if !rs.running {
		return time.Time{}
	}
	entries := rs.cron.Entries()
	if len(entries) > 0 {
		return entries[0].Next
	}
	return time.Time{}
}

// scheduleInterval attempts to determine the interval between scheduled runs
// Returns (interval, true) if successfully parsed, (0, false) otherwise
func (rs *RefundScheduler) scheduleInterval(now time.Time) (time.Duration, bool) {
	schedule := rs.node.AutoRefund.Schedule

	// Try parsing "@every <duration>" format using stdlib
	if strings.HasPrefix(schedule, "@every ") {
		durationStr := strings.TrimPrefix(schedule, "@every ")
		durationStr = strings.TrimSpace(durationStr)
		if duration, err := time.ParseDuration(durationStr); err == nil && duration > 0 {
			return duration, true
		}
	}

	// For standard cron expressions, use the next scheduled run time
	rs.mutex.RLock()
	defer rs.mutex.RUnlock()

	if rs.running {
		entries := rs.cron.Entries()
		if len(entries) > 0 {
			nextRun := entries[0].Next
			if !nextRun.IsZero() && nextRun.After(now) {
				return nextRun.Sub(now), true
			}
		}
	}

	return 0, false
}

// cycleTimeout computes the timeout for a single refund cycle
// Accounts for retry attempts to ensure each attempt gets adequate time
func (rs *RefundScheduler) cycleTimeout(now time.Time) time.Duration {
	// Calculate minimum timeout needed for retry logic:
	// - Total attempts = 1 initial + MAX_FAUCET_RETRIES retries
	// - Each attempt needs PER_ATTEMPT_TIMEOUT for confirmation
	// - Exponential backoff between retries: 1s, 4s, 9s... = sum of (attempt²)
	// - Add processing overhead buffer

	totalAttempts := 1 + MAX_FAUCET_RETRIES

	// Calculate total backoff time for exponential backoff (attempt * attempt seconds)
	// For 3 retries: 1² + 2² + 3² = 1 + 4 + 9 = 14 seconds
	totalBackoff := time.Duration(0)
	for attempt := 1; attempt <= MAX_FAUCET_RETRIES; attempt++ {
		totalBackoff += time.Duration(attempt*attempt) * time.Second
	}

	// Minimum timeout = (attempts × per-attempt timeout) + backoff + processing overhead
	minTimeout := time.Duration(totalAttempts)*PER_ATTEMPT_TIMEOUT + totalBackoff + PROCESSING_OVERHEAD_BUFFER

	// Start with the calculated minimum timeout
	calculatedTimeout := minTimeout

	// If config specifies a timeout, use the larger of the two
	if rs.node.AutoRefund.Timeout > 0 {
		configTimeout := time.Duration(rs.node.AutoRefund.Timeout) * time.Second
		if configTimeout > calculatedTimeout {
			calculatedTimeout = configTimeout
			logger.Debugf("%s Using config timeout %v (calculated min: %v)", rs.logPrefix(), configTimeout, minTimeout)
		} else {
			logger.Debugf("%s Using calculated timeout %v (config: %v insufficient for %d retries)",
				rs.logPrefix(), calculatedTimeout, configTimeout, MAX_FAUCET_RETRIES)
		}
	}

	// Try to get the schedule interval
	interval, ok := rs.scheduleInterval(now)
	if !ok || interval <= 0 {
		// Can't determine interval, use calculated timeout
		logger.Debugf("%s Using calculated timeout %v (schedule interval unknown)", rs.logPrefix(), calculatedTimeout)
		return calculatedTimeout
	}

	// Cap timeout to interval minus safety margin to prevent overlap
	if interval > CYCLE_SAFETY_MARGIN {
		intervalCap := interval - CYCLE_SAFETY_MARGIN
		if calculatedTimeout > intervalCap {
			logger.Warnf("%s Capping timeout from %v to %v based on schedule interval %v - retries may not complete",
				rs.logPrefix(), calculatedTimeout, intervalCap, interval)
			return intervalCap
		}
	}

	logger.Debugf("%s Using calculated timeout %v (schedule interval %v)", rs.logPrefix(), calculatedTimeout, interval)
	return calculatedTimeout
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
