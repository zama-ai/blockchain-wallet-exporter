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
	node             *config.Node
	faucetClient     faucet.Fauceter
	currencyRegistry *currency.Registry
	collector        collector.IModuleCollector
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

	// Calculate buffer size based on refund-enabled accounts for this node only
	refundAccountCount := 0
	for _, account := range node.Accounts {
		if account.RefundThreshold != nil && account.RefundTarget != nil {
			refundAccountCount++
		}
	}

	// Set buffer size with sensible minimum
	eventBufferSize := max(refundAccountCount, 10) // Smaller buffer for single node

	logger.Debugf("[scheduler %s] Node-specific scheduler configured for %d accounts with event buffer size %d",
		node.Name, refundAccountCount, eventBufferSize)

	scheduler := &RefundScheduler{
		node:             node,
		faucetClient:     faucetClient,
		currencyRegistry: currencyRegistry,
		collector:        nodeCollector,
		cron:             cron.New(cron.WithSeconds()),
		eventBufferSize:  eventBufferSize,
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

	// Cancel context and close stop channel
	rs.cancel()
	close(rs.stopChan)

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

	for _, event := range events {
		if event.Success {
			successCount++
			totalRefunded += event.RefundAmount
		} else {
			errorCount++
		}
	}

	duration := time.Since(startTime)
	logger.Infof("%s Auto-refund check completed in %v: %d successful, %d errors, %.0f wei total refunded",
		rs.logPrefix(), duration, successCount, errorCount, totalRefunded)
}

// processAccounts checks all accounts and refunds those below threshold
func (rs *RefundScheduler) processAccounts() []*RefundEvent {
	var events []*RefundEvent
	var wg sync.WaitGroup

	// Use pre-calculated buffer size - no more deadlocks!
	eventChan := make(chan *RefundEvent, rs.eventBufferSize)

	// Process each account in the node
	for _, account := range rs.node.Accounts {
		if account.RefundThreshold == nil || account.RefundTarget == nil {
			logger.Debugf("%s Skipping account %s - no refund configuration", rs.logPrefix(), account.Name)
			continue
		}

		wg.Add(1)
		go func(acc *config.Account) {
			defer wg.Done()
			event := rs.processAccount(acc)
			if event != nil {
				eventChan <- event // Now guaranteed to never block!
			}
		}(account)
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
func (rs *RefundScheduler) processAccount(account *config.Account) *RefundEvent {
	ctx, cancel := context.WithTimeout(rs.ctx, 30*time.Second)
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

	// Convert threshold and target from config unit to wei for faucet
	thresholdWei, err := rs.convertToWei(*account.RefundThreshold, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert threshold to wei: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to convert threshold to wei for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}
	logger.Debugf("%s Threshold conversion: %.6f %s -> %.0f wei", rs.logPrefix(), *account.RefundThreshold, collectorUnit, thresholdWei)

	targetWei, err := rs.convertToWei(*account.RefundTarget, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert target to wei: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to convert target to wei for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}
	logger.Debugf("%s Target conversion: %.6f %s -> %.0f wei", rs.logPrefix(), *account.RefundTarget, collectorUnit, targetWei)

	// Convert collector balance to wei for comparison
	currentBalanceWei, err := rs.convertToWei(result.Value, collectorUnit)
	if err != nil {
		event.Error = fmt.Errorf("failed to convert current balance to wei: %w", err)
		event.Duration = time.Since(startTime)
		logger.Errorf("%s Failed to convert current balance to wei for account %s: %v", rs.logPrefix(), account.Name, err)
		return event
	}
	logger.Debugf("%s Balance conversion: %.6f %s -> %.0f wei", rs.logPrefix(), result.Value, collectorUnit, currentBalanceWei)

	// Update event with wei amount
	event.CurrentBalance = currentBalanceWei

	// Compare in wei
	if currentBalanceWei >= thresholdWei {
		logger.Debugf("%s Account %s balance %.6f %s (%.0f wei) is above threshold %.6f %s (%.0f wei), no refund needed",
			rs.logPrefix(), account.Name, result.Value, collectorUnit, currentBalanceWei, *account.RefundThreshold, collectorUnit, thresholdWei)
		return nil
	}

	// Calculate refund amount in wei
	refundAmountWei := targetWei - currentBalanceWei
	if refundAmountWei <= 0 {
		logger.Warnf("%s Invalid refund amount %.0f wei for account %s", rs.logPrefix(), refundAmountWei, account.Name)
		return nil
	}
	logger.Debugf("%s Refund calculation: %.0f (target) - %.0f (current) = %.0f wei", rs.logPrefix(), targetWei, currentBalanceWei, refundAmountWei)

	// Convert values to human-readable units for display purposes only
	currentBalanceDisplay, err := rs.convertFromWei(currentBalanceWei, collectorUnit)
	if err != nil {
		logger.Warnf("%s Failed to convert current balance for display: %v", rs.logPrefix(), err)
		currentBalanceDisplay = currentBalanceWei // fallback to wei
	}

	refundAmountDisplay, err := rs.convertFromWei(refundAmountWei, collectorUnit)
	if err != nil {
		logger.Warnf("%s Failed to convert refund amount for display: %v", rs.logPrefix(), err)
		refundAmountDisplay = refundAmountWei // fallback to wei
	}

	// Update event with refund details
	event.RefundAmount = refundAmountWei
	event.AmountWei = refundAmountWei
	event.SourceUnit = "wei"
	event.Threshold = *account.RefundThreshold
	event.TargetAmount = *account.RefundTarget

	logger.Infof("%s Account %s balance %.6f %s (%.0f wei) is below threshold %.6f %s, refunding %.6f %s (%.0f wei) to reach target %.6f %s",
		rs.logPrefix(), account.Name, currentBalanceDisplay, collectorUnit, currentBalanceWei, *account.RefundThreshold, collectorUnit,
		refundAmountDisplay, collectorUnit, refundAmountWei, *account.RefundTarget, collectorUnit)

	// Call faucet directly with wei amount (specify unit as wei to avoid conversion)
	logger.Debugf("%s Calling faucet with amount: %.0f wei for account %s", rs.logPrefix(), refundAmountWei, account.Address)
	faucetResult, err := rs.faucetClient.FundAccountWeiWithRetry(ctx, account.Address, refundAmountWei, 2)
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
		logMsg := fmt.Sprintf("%s Successfully refunded account %s with %.6f %s (%.0f wei), session: %s, status: %s, claim status: %s",
			rs.logPrefix(), account.Name, refundAmountDisplay, collectorUnit, event.AmountWei, event.Session, event.Status, event.ClaimStatus)

		if event.Confirmed && event.ClaimHash != "" {
			logMsg += fmt.Sprintf(", confirmed with tx: %s at block %d", event.ClaimHash, event.ClaimBlock)
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
