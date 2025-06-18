package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/collector"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/faucet"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

// SchedulerManager manages multiple per-node refund schedulers
type SchedulerManager struct {
	config           *config.Schema
	currencyRegistry *currency.Registry
	schedulers       map[string]*RefundScheduler // node name -> scheduler
	collectors       map[string]collector.IModuleCollector
	faucetClients    map[string]faucet.Fauceter // node name -> faucet client

	running bool
	mutex   sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// SchedulerInfo contains information about a running scheduler
type SchedulerInfo struct {
	NodeName       string
	IsRunning      bool
	NextRun        time.Time
	FaucetURL      string
	Schedule       string
	AccountCount   int
	RefundAccounts int
}

// logPrefix returns a consistent log prefix for the manager
func (sm *SchedulerManager) logPrefix() string {
	return "[manager]"
}

// logPrefixNode returns a consistent log prefix for node-specific operations
func (sm *SchedulerManager) logPrefixNode(nodeName string) string {
	return fmt.Sprintf("[manager %s]", nodeName)
}

// NewSchedulerManager creates a new scheduler manager
func NewSchedulerManager(cfg *config.Schema, currencyRegistry *currency.Registry) (*SchedulerManager, error) {
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

	manager := &SchedulerManager{
		config:           cfg,
		currencyRegistry: currencyRegistry,
		schedulers:       make(map[string]*RefundScheduler),
		collectors:       collectors,
		faucetClients:    make(map[string]faucet.Fauceter),
		ctx:              ctx,
		cancel:           cancel,
	}

	return manager, nil
}

// Start initializes and starts all enabled schedulers
func (sm *SchedulerManager) Start() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.running {
		return fmt.Errorf("scheduler manager is already running")
	}

	logger.Infof("%s Starting scheduler manager...", sm.logPrefix())

	schedulersStarted := 0
	for _, node := range sm.config.Nodes {
		// Skip nodes that don't have auto-refund enabled
		if !node.IsAutoRefundEnabled() {
			logger.Debugf("%s Skipping scheduler for node %s: auto-refund disabled or not properly configured", sm.logPrefix(), node.Name)
			continue
		}

		// Create faucet client for this node
		faucetTimeout := time.Duration(node.AutoRefund.Timeout) * time.Second
		faucetClient := faucet.NewClient(node.AutoRefund.FaucetURL, faucetTimeout)
		sm.faucetClients[node.Name] = faucetClient

		// Create node-specific scheduler
		scheduler, err := sm.createNodeScheduler(node, faucetClient)
		if err != nil {
			logger.Errorf("%s Failed to create scheduler for node %s: %v", sm.logPrefixNode(node.Name), node.Name, err)
			continue
		}

		// Start the scheduler
		if err := scheduler.Start(); err != nil {
			logger.Errorf("%s Failed to start scheduler for node %s: %v", sm.logPrefixNode(node.Name), node.Name, err)
			continue
		}

		sm.schedulers[node.Name] = scheduler
		schedulersStarted++

		refundAccountCount := 0
		for _, account := range node.Accounts {
			if account.RefundThreshold != nil && account.RefundTarget != nil {
				refundAccountCount++
			}
		}

		logger.Infof("%s Started scheduler for node '%s' with %d refund-enabled accounts (schedule: %s, faucet: %s)",
			sm.logPrefixNode(node.Name), node.Name, refundAccountCount, node.AutoRefund.Schedule, node.AutoRefund.FaucetURL)
	}

	if schedulersStarted == 0 {
		return fmt.Errorf("no schedulers could be started - check that nodes have explicit autoRefund configuration")
	}

	sm.running = true
	logger.Infof("%s Scheduler manager started successfully with %d active schedulers", sm.logPrefix(), schedulersStarted)
	return nil
}

// Stop stops all running schedulers
func (sm *SchedulerManager) Stop() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if !sm.running {
		return nil
	}

	logger.Infof("%s Stopping scheduler manager...", sm.logPrefix())

	// Stop all schedulers
	var wg sync.WaitGroup
	for nodeName, scheduler := range sm.schedulers {
		wg.Add(1)
		go func(name string, s *RefundScheduler) {
			defer wg.Done()
			if err := s.Stop(); err != nil {
				logger.Errorf("%s Failed to stop scheduler for node %s: %v", sm.logPrefixNode(name), name, err)
			} else {
				logger.Debugf("%s Stopped scheduler for node %s", sm.logPrefixNode(name), name)
			}
		}(nodeName, scheduler)
	}

	wg.Wait()

	// Cancel context and cleanup
	sm.cancel()

	// Close collectors
	for nodeName, collector := range sm.collectors {
		if err := collector.Close(); err != nil {
			logger.Errorf("%s Failed to close collector for node %s: %v", sm.logPrefixNode(nodeName), nodeName, err)
		}
	}

	sm.schedulers = make(map[string]*RefundScheduler)
	sm.faucetClients = make(map[string]faucet.Fauceter)
	sm.running = false

	logger.Infof("%s Scheduler manager stopped", sm.logPrefix())
	return nil
}

// IsRunning returns whether the manager is currently running
func (sm *SchedulerManager) IsRunning() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.running
}

// GetSchedulerInfo returns information about all running schedulers
func (sm *SchedulerManager) GetSchedulerInfo() []SchedulerInfo {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	var infos []SchedulerInfo
	for _, node := range sm.config.Nodes {
		scheduler, exists := sm.schedulers[node.Name]

		refundAccountCount := 0
		for _, account := range node.Accounts {
			if account.RefundThreshold != nil && account.RefundTarget != nil {
				refundAccountCount++
			}
		}

		info := SchedulerInfo{
			NodeName:       node.Name,
			AccountCount:   len(node.Accounts),
			RefundAccounts: refundAccountCount,
		}

		// Only populate faucet info if node has autoRefund configured
		if node.AutoRefund != nil {
			info.FaucetURL = node.AutoRefund.FaucetURL
			info.Schedule = node.AutoRefund.Schedule
		}

		if exists && scheduler != nil {
			info.IsRunning = scheduler.IsRunning()
			info.NextRun = scheduler.GetNextRun()
		}

		infos = append(infos, info)
	}

	return infos
}

// createNodeScheduler creates a scheduler for a specific node
func (sm *SchedulerManager) createNodeScheduler(node *config.Node, faucetClient faucet.Fauceter) (*RefundScheduler, error) {
	collector, exists := sm.collectors[node.Name]
	if !exists {
		return nil, fmt.Errorf("no collector found for node %s", node.Name)
	}

	// Create node-specific scheduler with the simplified interface
	scheduler, err := NewNodeRefundScheduler(node, sm.currencyRegistry, faucetClient, collector)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler for node %s: %w", node.Name, err)
	}

	return scheduler, nil
}

// GetNodeScheduler returns the scheduler for a specific node
func (sm *SchedulerManager) GetNodeScheduler(nodeName string) (*RefundScheduler, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	scheduler, exists := sm.schedulers[nodeName]
	return scheduler, exists
}

// RestartNodeScheduler restarts the scheduler for a specific node
func (sm *SchedulerManager) RestartNodeScheduler(nodeName string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if !sm.running {
		return fmt.Errorf("scheduler manager is not running")
	}

	// Find the node configuration
	var targetNode *config.Node
	for _, node := range sm.config.Nodes {
		if node.Name == nodeName {
			targetNode = node
			break
		}
	}

	if targetNode == nil {
		return fmt.Errorf("node %s not found in configuration", nodeName)
	}

	// Stop existing scheduler if it exists
	if scheduler, exists := sm.schedulers[nodeName]; exists {
		if err := scheduler.Stop(); err != nil {
			logger.Warnf("%s Error stopping existing scheduler for node %s: %v", sm.logPrefixNode(nodeName), nodeName, err)
		}
		delete(sm.schedulers, nodeName)
	}

	// Check if node has auto-refund enabled
	if !targetNode.IsAutoRefundEnabled() {
		return fmt.Errorf("auto-refund not enabled for node %s or not properly configured", nodeName)
	}

	// Create new faucet client
	faucetTimeout := time.Duration(targetNode.AutoRefund.Timeout) * time.Second
	faucetClient := faucet.NewClient(targetNode.AutoRefund.FaucetURL, faucetTimeout)
	sm.faucetClients[nodeName] = faucetClient

	// Create and start new scheduler
	scheduler, err := sm.createNodeScheduler(targetNode, faucetClient)
	if err != nil {
		return fmt.Errorf("failed to create scheduler for node %s: %w", nodeName, err)
	}

	if err := scheduler.Start(); err != nil {
		return fmt.Errorf("failed to start scheduler for node %s: %w", nodeName, err)
	}

	sm.schedulers[nodeName] = scheduler
	logger.Infof("%s Restarted scheduler for node %s", sm.logPrefixNode(nodeName), nodeName)
	return nil
}
