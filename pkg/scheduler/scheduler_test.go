package scheduler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/collector"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/faucet"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

// mockModuleCollector is a mock implementation of IModuleCollector for testing.
type mockModuleCollector struct {
	balance        float64
	err            error
	closeCalled    bool
	healthOverride *float64 // Allow overriding health for specific tests
	collectFunc    func(ctx context.Context, account *config.Account) (*collector.BaseResult, error)
}

func init() {
	_ = logger.InitLogger()
}

func (m *mockModuleCollector) CollectAccountBalance(ctx context.Context, account *config.Account) (*collector.BaseResult, error) {
	if m.collectFunc != nil {
		return m.collectFunc(ctx, account)
	}
	if m.err != nil {
		return nil, m.err
	}
	health := 1.0
	if m.healthOverride != nil {
		health = *m.healthOverride
	}
	return &collector.BaseResult{
		NodeName: "test-node",
		Account:  *account,
		Value:    m.balance,
		Health:   health,
	}, nil
}

func (m *mockModuleCollector) Close() error {
	m.closeCalled = true
	return nil
}

func (m *mockModuleCollector) Name() string {
	return "mock"
}

// mockFauceter is a mock implementation of Fauceter for testing.
type mockFauceter struct {
	fundWithRetryFunc func(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error)
}

func (m *mockFauceter) FundAccountWeiWithRetry(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error) {
	if m.fundWithRetryFunc != nil {
		return m.fundWithRetryFunc(ctx, address, amountWei, maxRetries)
	}
	return nil, fmt.Errorf("mock FundAccountWeiWithRetry not implemented")
}

// Helper function to create a valid node configuration for testing
func createTestNodeConfig(nodeName string, autoRefundEnabled bool) *config.Node {
	node := &config.Node{
		Name:     nodeName,
		Module:   "evm",                   // Required for collector creation
		HttpAddr: "http://test-node:8545", // Required for EVM collector
		Labels:   make(map[string]string), // Required for collector to prevent nil map assignment
		Unit: &currency.Unit{
			Name:     "eth",
			Symbol:   "ETH",
			Decimals: 18,
		},
		MetricsUnit: &currency.Unit{
			Name:     "eth",
			Symbol:   "ETH",
			Decimals: 18,
		},
		Accounts: []*config.Account{
			{
				Address: "0x1234567890123456789012345678901234567890",
				Name:    "test-account",
			},
		},
	}

	// Add some test labels
	node.Labels["env"] = "test"
	node.Labels["type"] = "mock"

	if autoRefundEnabled {
		refundThreshold := 5.0
		refundTarget := 10.0
		node.AutoRefund = &config.AutoRefund{
			Enabled:   true,
			FaucetURL: "http://test-faucet:8080",
			Schedule:  "@every 1m",
			Timeout:   30,
		}
		node.Accounts[0].RefundThreshold = &refundThreshold
		node.Accounts[0].RefundTarget = &refundTarget
	}

	return node
}

func TestProcessAccount_NoRefundNeeded(t *testing.T) {
	mockCollector := &mockModuleCollector{
		balance: 10.0, // 10 ETH, above threshold
	}
	mockFaucet := &mockFauceter{
		fundWithRetryFunc: func(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error) {
			t.Error("Faucet should not be called when balance is sufficient")
			return nil, nil
		},
	}

	currencyRegistry := currency.NewDefaultRegistry()
	node := createTestNodeConfig("test-node", true)

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	refundThreshold := 5.0
	refundTarget := 10.0
	account := &config.Account{
		Address:         "0x1234567890123456789012345678901234567890",
		Name:            "test-account",
		RefundThreshold: &refundThreshold,
		RefundTarget:    &refundTarget,
	}

	event := rs.processAccount(account)

	if event != nil {
		t.Errorf("Expected no refund event, but got one: %+v", event)
	}
}

func TestProcessAccount_RefundSuccessful(t *testing.T) {
	var faucetCalled bool
	expectedAmountWei := 8e18 // 10 (target) - 2 (current) = 8 ETH

	mockCollector := &mockModuleCollector{
		balance: 2.0, // 2 ETH, below threshold
	}
	mockFaucet := &mockFauceter{
		fundWithRetryFunc: func(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error) {
			faucetCalled = true
			if amountWei != expectedAmountWei {
				return nil, fmt.Errorf("expected amount %f, got %f", expectedAmountWei, amountWei)
			}
			return &faucet.FaucetResult{
				Success: true,
			}, nil
		},
	}

	currencyRegistry := currency.NewDefaultRegistry()
	node := createTestNodeConfig("test-node", true)

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	refundThreshold := 5.0
	refundTarget := 10.0
	account := &config.Account{
		Address:         "0x1234567890123456789012345678901234567890",
		Name:            "test-account",
		RefundThreshold: &refundThreshold,
		RefundTarget:    &refundTarget,
	}

	event := rs.processAccount(account)

	if !faucetCalled {
		t.Error("Expected faucet to be called, but it wasn't")
	}

	if event == nil {
		t.Fatal("Expected a refund event, but got nil")
	}
	if !event.Success {
		t.Errorf("Expected refund to be successful, but it failed: %v", event.Error)
	}
	if event.AmountWei != expectedAmountWei {
		t.Errorf("Expected event AmountWei to be %f, but got %f", expectedAmountWei, event.AmountWei)
	}
}

func TestProcessAccount_CollectorError(t *testing.T) {
	mockCollector := &mockModuleCollector{
		err: fmt.Errorf("collector connection failed"),
	}
	mockFaucet := &mockFauceter{}

	currencyRegistry := currency.NewDefaultRegistry()
	node := createTestNodeConfig("test-node", true)

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	refundThreshold := 5.0
	refundTarget := 10.0
	account := &config.Account{
		Address:         "0x1234567890123456789012345678901234567890",
		Name:            "test-account",
		RefundThreshold: &refundThreshold,
		RefundTarget:    &refundTarget,
	}

	event := rs.processAccount(account)

	if event == nil {
		t.Fatal("Expected an error event, but got nil")
	}
	if event.Error == nil {
		t.Error("Expected error in event, but got none")
	}
	if !strings.Contains(fmt.Sprintf("%v", event.Error), "failed to get balance") {
		t.Errorf("Expected error message about balance, got: %v", event.Error)
	}
}

func TestProcessAccount_HealthCheckFailed(t *testing.T) {
	healthZero := 0.0
	mockCollector := &mockModuleCollector{
		balance:        2.0,
		healthOverride: &healthZero, // Health check fails
	}

	mockFaucet := &mockFauceter{}

	currencyRegistry := currency.NewDefaultRegistry()
	node := createTestNodeConfig("test-node", true)

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	refundThreshold := 5.0
	refundTarget := 10.0
	account := &config.Account{
		Address:         "0x1234567890123456789012345678901234567890",
		Name:            "test-account",
		RefundThreshold: &refundThreshold,
		RefundTarget:    &refundTarget,
	}

	event := rs.processAccount(account)

	if event == nil {
		t.Fatal("Expected an error event, but got nil")
	}
	if event.Error == nil {
		t.Error("Expected error in event, but got none")
	}
	if !strings.Contains(fmt.Sprintf("%v", event.Error), "account health check failed") {
		t.Errorf("Expected health check error, got: %v", event.Error)
	}
}

func TestProcessAccount_FaucetError(t *testing.T) {
	mockCollector := &mockModuleCollector{
		balance: 2.0, // Below threshold
	}
	mockFaucet := &mockFauceter{
		fundWithRetryFunc: func(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error) {
			return nil, fmt.Errorf("faucet service unavailable")
		},
	}

	currencyRegistry := currency.NewDefaultRegistry()
	node := createTestNodeConfig("test-node", true)

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	refundThreshold := 5.0
	refundTarget := 10.0
	account := &config.Account{
		Address:         "0x1234567890123456789012345678901234567890",
		Name:            "test-account",
		RefundThreshold: &refundThreshold,
		RefundTarget:    &refundTarget,
	}

	event := rs.processAccount(account)

	if event == nil {
		t.Fatal("Expected an error event, but got nil")
	}
	if event.Error == nil {
		t.Error("Expected error in event, but got none")
	}
	if !strings.Contains(fmt.Sprintf("%v", event.Error), "failed to fund account") {
		t.Errorf("Expected faucet error, got: %v", event.Error)
	}
}

func TestConvertToWei(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	rs := &RefundScheduler{
		currencyRegistry: currencyRegistry,
	}

	tests := []struct {
		name       string
		amount     float64
		sourceUnit string
		expected   float64
		expectErr  bool
	}{
		{
			name:       "wei to wei",
			amount:     1000,
			sourceUnit: "wei",
			expected:   1000,
			expectErr:  false,
		},
		{
			name:       "eth to wei",
			amount:     1.0,
			sourceUnit: "eth",
			expected:   1e18,
			expectErr:  false,
		},
		{
			name:       "gwei to wei",
			amount:     1.0,
			sourceUnit: "gwei",
			expected:   1e9,
			expectErr:  false,
		},
		{
			name:       "invalid unit",
			amount:     1.0,
			sourceUnit: "invalid",
			expected:   0,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := rs.convertToWei(tt.amount, tt.sourceUnit)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", tt.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %f, got %f for %s", tt.expected, result, tt.name)
				}
			}
		})
	}
}

func TestConvertFromWei(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	rs := &RefundScheduler{
		currencyRegistry: currencyRegistry,
	}

	tests := []struct {
		name       string
		amountWei  float64
		targetUnit string
		expected   float64
		expectErr  bool
	}{
		{
			name:       "wei to wei",
			amountWei:  1000,
			targetUnit: "wei",
			expected:   1000,
			expectErr:  false,
		},
		{
			name:       "wei to eth",
			amountWei:  1e18,
			targetUnit: "eth",
			expected:   1.0,
			expectErr:  false,
		},
		{
			name:       "wei to gwei",
			amountWei:  1e9,
			targetUnit: "gwei",
			expected:   1.0,
			expectErr:  false,
		},
		{
			name:       "invalid unit",
			amountWei:  1000,
			targetUnit: "invalid",
			expected:   0,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := rs.convertFromWei(tt.amountWei, tt.targetUnit)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", tt.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %f, got %f for %s", tt.expected, result, tt.name)
				}
			}
		})
	}
}

func TestNewNodeRefundScheduler(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}
	mockCollector := &mockModuleCollector{}

	t.Run("AutoRefundDisabled", func(t *testing.T) {
		node := createTestNodeConfig("test-node", false) // autoRefund disabled
		_, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
		if err == nil {
			t.Error("Expected error when auto-refund is disabled, but got nil")
		}
		expectedMsg := "auto-refund is not enabled or properly configured for node test-node"
		if err.Error() != expectedMsg {
			t.Errorf("Expected error message '%s', but got '%s'", expectedMsg, err.Error())
		}
	})

	t.Run("NoFaucetURL", func(t *testing.T) {
		node := createTestNodeConfig("test-node", true)
		node.AutoRefund.FaucetURL = "" // Remove faucet URL
		_, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
		if err == nil {
			t.Error("Expected error when faucet URL is missing, but got nil")
		}
		expectedMsg := "auto-refund is not enabled or properly configured for node test-node"
		if err.Error() != expectedMsg {
			t.Errorf("Expected error message '%s', but got '%s'", expectedMsg, err.Error())
		}
	})

	t.Run("NoRefundAccounts", func(t *testing.T) {
		node := createTestNodeConfig("test-node", true)
		// Remove refund thresholds
		node.Accounts[0].RefundThreshold = nil
		node.Accounts[0].RefundTarget = nil
		_, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
		if err == nil {
			t.Error("Expected error when no refund accounts configured, but got nil")
		}
		expectedMsg := "auto-refund is not enabled or properly configured for node test-node"
		if err.Error() != expectedMsg {
			t.Errorf("Expected error message '%s', but got '%s'", expectedMsg, err.Error())
		}
	})

	t.Run("ValidConfiguration", func(t *testing.T) {
		node := createTestNodeConfig("test-node", true)
		scheduler, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
		if err != nil {
			t.Errorf("Expected no error for valid configuration, but got: %v", err)
		}
		if scheduler == nil {
			t.Error("Expected scheduler to be created, but got nil")
			return
		}
		if scheduler.node.Name != "test-node" {
			t.Errorf("Expected scheduler node name to be 'test-node', but got '%s'", scheduler.node.Name)
		}
	})
}

func TestNodeIsAutoRefundEnabled(t *testing.T) {
	tests := []struct {
		name     string
		node     *config.Node
		expected bool
	}{
		{
			name:     "AutoRefundEnabled",
			node:     createTestNodeConfig("test-node", true),
			expected: true,
		},
		{
			name:     "AutoRefundDisabled",
			node:     createTestNodeConfig("test-node", false),
			expected: false,
		},
		{
			name: "NoAutoRefundConfig",
			node: &config.Node{
				Name:     "test-node",
				Accounts: []*config.Account{},
			},
			expected: false,
		},
		{
			name: "EnabledButNoFaucetURL",
			node: &config.Node{
				Name: "test-node",
				AutoRefund: &config.AutoRefund{
					Enabled:   true,
					FaucetURL: "", // Missing URL
				},
				Accounts: []*config.Account{
					{
						Address: "0x123",
						Name:    "test",
					},
				},
			},
			expected: false,
		},
		{
			name: "EnabledButNoRefundAccounts",
			node: &config.Node{
				Name: "test-node",
				AutoRefund: &config.AutoRefund{
					Enabled:   true,
					FaucetURL: "http://faucet:8080",
				},
				Accounts: []*config.Account{
					{
						Address: "0x123",
						Name:    "test",
						// No RefundThreshold/RefundTarget
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.node.IsAutoRefundEnabled()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for %s", tt.expected, result, tt.name)
			}
		})
	}
}

func TestNodeHasRefundEnabledAccounts(t *testing.T) {
	refundThreshold := 5.0
	refundTarget := 10.0

	tests := []struct {
		name     string
		node     *config.Node
		expected bool
	}{
		{
			name: "HasRefundEnabledAccounts",
			node: &config.Node{
				Accounts: []*config.Account{
					{
						Address:         "0x123",
						RefundThreshold: &refundThreshold,
						RefundTarget:    &refundTarget,
					},
				},
			},
			expected: true,
		},
		{
			name: "NoRefundEnabledAccounts",
			node: &config.Node{
				Accounts: []*config.Account{
					{
						Address: "0x123",
						// No refund config
					},
				},
			},
			expected: false,
		},
		{
			name: "EmptyAccounts",
			node: &config.Node{
				Accounts: []*config.Account{},
			},
			expected: false,
		},
		{
			name: "MixedAccounts",
			node: &config.Node{
				Accounts: []*config.Account{
					{
						Address: "0x123",
						// No refund config
					},
					{
						Address:         "0x456",
						RefundThreshold: &refundThreshold,
						RefundTarget:    &refundTarget,
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.node.HasRefundEnabledAccounts()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for %s", tt.expected, result, tt.name)
			}
		})
	}
}

func TestSchedulerIsRunning(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}
	mockCollector := &mockModuleCollector{}

	node := createTestNodeConfig("test-node", true)

	rs, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	// Initially not running
	if rs.IsRunning() {
		t.Error("Expected scheduler to not be running initially")
	}

	// Start scheduler
	err = rs.Start()
	if err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	// Should be running now
	if !rs.IsRunning() {
		t.Error("Expected scheduler to be running after start")
	}

	// Stop scheduler
	err = rs.Stop()
	if err != nil {
		t.Fatalf("Failed to stop scheduler: %v", err)
	}

	// Should not be running anymore
	if rs.IsRunning() {
		t.Error("Expected scheduler to not be running after stop")
	}
}

func TestSchedulerStartStop(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}
	mockCollector := &mockModuleCollector{}

	node := createTestNodeConfig("test-node", true)

	rs, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	// Test starting twice
	err = rs.Start()
	if err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}

	err = rs.Start()
	if err == nil {
		t.Error("Expected error when starting already running scheduler")
	}

	// Test stopping twice
	err = rs.Stop()
	if err != nil {
		t.Fatalf("Failed to stop scheduler: %v", err)
	}

	err = rs.Stop()
	if err != nil {
		t.Errorf("Unexpected error when stopping already stopped scheduler: %v", err)
	}
}

func TestSchedulerGetNextRun(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}
	mockCollector := &mockModuleCollector{}

	node := createTestNodeConfig("test-node", true)

	rs, err := NewNodeRefundScheduler(node, currencyRegistry, mockFaucet, mockCollector)
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	// When not running, should return zero time
	nextRun := rs.GetNextRun()
	if !nextRun.IsZero() {
		t.Error("Expected zero time when scheduler is not running")
	}

	// Start scheduler
	err = rs.Start()
	if err != nil {
		t.Fatalf("Failed to start scheduler: %v", err)
	}
	defer func() {
		if stopErr := rs.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scheduler: %v", stopErr)
		}
	}()

	// When running, should return a future time
	nextRun = rs.GetNextRun()
	if nextRun.IsZero() {
		t.Error("Expected non-zero time when scheduler is running")
	}
	if nextRun.Before(time.Now()) {
		t.Error("Expected next run to be in the future")
	}
}

func TestSchedulerProcessAccounts(t *testing.T) {
	mockCollector := &mockModuleCollector{
		balance: 2.0, // Below threshold
	}
	mockFaucet := &mockFauceter{
		fundWithRetryFunc: func(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error) {
			return &faucet.FaucetResult{
				Success: true,
			}, nil
		},
	}

	currencyRegistry := currency.NewDefaultRegistry()

	// Create node with multiple accounts
	node := createTestNodeConfig("test-node", true)
	refundThreshold := 5.0
	refundTarget := 10.0

	// Add a second account
	node.Accounts = append(node.Accounts, &config.Account{
		Address:         "0x1234567890123456789012345678901234567891",
		Name:            "test-account-2",
		RefundThreshold: &refundThreshold,
		RefundTarget:    &refundTarget,
	})

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	events := rs.processAccounts()

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	for _, event := range events {
		if !event.Success {
			t.Errorf("Expected successful refund event, got: %v", event.Error)
		}
	}
}

func TestSchedulerProcessAccounts_ConcurrencyLimit(t *testing.T) {
	var activeWorkers int32
	var maxActiveWorkers int32
	var mu sync.Mutex

	// Mock collector that simulates slow processing to test concurrency
	mockCollector := &mockModuleCollector{
		balance: 2.0, // Below threshold to trigger refund
	}

	// Override the CollectAccountBalance to simulate slow processing
	mockCollector.collectFunc = func(ctx context.Context, account *config.Account) (*collector.BaseResult, error) {
		// Track active workers
		current := atomic.AddInt32(&activeWorkers, 1)
		defer atomic.AddInt32(&activeWorkers, -1)

		// Track max concurrency
		mu.Lock()
		if current > maxActiveWorkers {
			maxActiveWorkers = current
		}
		mu.Unlock()

		// Simulate slow work
		time.Sleep(100 * time.Millisecond)

		// Return default result like the original method would
		health := 1.0
		if mockCollector.healthOverride != nil {
			health = *mockCollector.healthOverride
		}
		return &collector.BaseResult{
			NodeName: "test-node",
			Account:  *account,
			Value:    mockCollector.balance,
			Health:   health,
		}, nil
	}

	mockFaucet := &mockFauceter{
		fundWithRetryFunc: func(ctx context.Context, address string, amountWei float64, maxRetries int) (*faucet.FaucetResult, error) {
			return &faucet.FaucetResult{
				Success: true,
			}, nil
		},
	}

	currencyRegistry := currency.NewDefaultRegistry()

	// Create node with multiple accounts (more than MAX_WORKER_GOROUTINES)
	node := createTestNodeConfig("test-node", true)
	refundThreshold := 5.0
	refundTarget := 10.0

	// Add 25 accounts (more than MAX_WORKER_GOROUTINES which is 20)
	node.Accounts = []*config.Account{}
	for i := 0; i < 25; i++ {
		account := &config.Account{
			Address:         fmt.Sprintf("0x123456789012345678901234567890123456789%d", i),
			Name:            fmt.Sprintf("test-account-%d", i),
			RefundThreshold: &refundThreshold,
			RefundTarget:    &refundTarget,
		}
		node.Accounts = append(node.Accounts, account)
	}

	rs := &RefundScheduler{
		node:             node,
		collector:        mockCollector,
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
	}

	logger.Infof("Testing concurrency limit with %d accounts and max %d workers", len(node.Accounts), MAX_WORKER_GOROUTINES)

	start := time.Now()
	events := rs.processAccounts()
	duration := time.Since(start)

	// Verify results
	if len(events) != 25 {
		t.Errorf("Expected 25 events, got %d", len(events))
	}

	// Verify concurrency was limited
	mu.Lock()
	finalMaxWorkers := maxActiveWorkers
	mu.Unlock()

	if finalMaxWorkers > int32(MAX_WORKER_GOROUTINES) {
		t.Errorf("Maximum active workers %d exceeded limit %d", finalMaxWorkers, MAX_WORKER_GOROUTINES)
	}

	// Verify we actually used concurrent processing (should be more than 1 worker)
	if finalMaxWorkers < 2 {
		t.Errorf("Expected concurrent processing with multiple workers, but max was only %d", finalMaxWorkers)
	}

	// With 20 max workers processing 25 accounts, and each taking ~100ms,
	// it should take roughly 200ms (25/20 = 1.25 rounds, so 2 * 100ms)
	// Allow some buffer for test environment variations
	expectedMinDuration := 150 * time.Millisecond
	expectedMaxDuration := 350 * time.Millisecond

	if duration < expectedMinDuration {
		t.Errorf("Processing completed too quickly (%v), suggesting no concurrency limit was applied", duration)
	}
	if duration > expectedMaxDuration {
		t.Logf("Warning: Processing took longer than expected (%v), but this might be due to test environment", duration)
	}

	logger.Infof("Concurrency test completed: %d accounts processed in %v with max %d concurrent workers",
		len(events), duration, finalMaxWorkers)
}

func TestSchedulerManager_NewSchedulerManager(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()

	t.Run("ValidConfiguration", func(t *testing.T) {
		cfg := &config.Schema{
			Nodes: []*config.Node{
				createTestNodeConfig("test-node-1", true),
				createTestNodeConfig("test-node-2", true),
			},
		}

		manager, err := NewSchedulerManager(cfg, currencyRegistry)
		if err != nil {
			t.Errorf("Unexpected error for valid configuration: %v", err)
		}
		if manager == nil {
			t.Error("Expected manager to be created, but got nil")
		}
	})

	t.Run("EmptyConfiguration", func(t *testing.T) {
		cfg := &config.Schema{
			Nodes: []*config.Node{},
		}

		manager, err := NewSchedulerManager(cfg, currencyRegistry)
		if err != nil {
			t.Errorf("Unexpected error for empty configuration: %v", err)
		}
		if manager == nil {
			t.Error("Expected manager to be created, but got nil")
		}
	})
}

func TestSchedulerManager_StartStop(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()

	cfg := &config.Schema{
		Nodes: []*config.Node{
			createTestNodeConfig("test-node-1", true),
			createTestNodeConfig("test-node-2", false), // Disabled
		},
	}

	manager, err := NewSchedulerManager(cfg, currencyRegistry)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Test starting
	err = manager.Start()
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	if !manager.IsRunning() {
		t.Error("Expected manager to be running")
	}

	// Test stopping
	err = manager.Stop()
	if err != nil {
		t.Fatalf("Failed to stop manager: %v", err)
	}

	if manager.IsRunning() {
		t.Error("Expected manager to not be running after stop")
	}
}

func TestSchedulerManager_GetSchedulerInfo(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()

	cfg := &config.Schema{
		Nodes: []*config.Node{
			createTestNodeConfig("enabled-node", true),
			createTestNodeConfig("disabled-node", false),
		},
	}

	manager, err := NewSchedulerManager(cfg, currencyRegistry)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	infos := manager.GetSchedulerInfo()

	if len(infos) != 2 {
		t.Errorf("Expected 2 scheduler infos, got %d", len(infos))
	}

	for _, info := range infos {
		switch info.NodeName {
		case "enabled-node":
			if info.FaucetURL == "" {
				t.Error("Expected faucet URL for enabled node")
			}
			if info.Schedule == "" {
				t.Error("Expected schedule for enabled node")
			}
		case "disabled-node":
			if info.FaucetURL != "" || info.Schedule != "" {
				t.Error("Did not expect faucet config for disabled node")
			}
		}
	}
}
