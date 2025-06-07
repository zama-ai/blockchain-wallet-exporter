package scheduler

import (
	"context"
	"fmt"
	"strings"
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
}

func init() {
	_ = logger.InitLogger()
}

func (m *mockModuleCollector) CollectAccountBalance(ctx context.Context, account *config.Account) (*collector.BaseResult, error) {
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
	rs := &RefundScheduler{
		collectors:       map[string]collector.IModuleCollector{"test-node": mockCollector},
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
	node := &config.Node{
		Name: "test-node",
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
	}

	event := rs.processAccount(node, account, mockCollector)

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

	rs := &RefundScheduler{
		collectors:       map[string]collector.IModuleCollector{"test-node": mockCollector},
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
	node := &config.Node{
		Name: "test-node",
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
	}

	event := rs.processAccount(node, account, mockCollector)

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
	rs := &RefundScheduler{
		collectors:       map[string]collector.IModuleCollector{"test-node": mockCollector},
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
	node := &config.Node{
		Name: "test-node",
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
	}

	event := rs.processAccount(node, account, mockCollector)

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
	rs := &RefundScheduler{
		collectors:       map[string]collector.IModuleCollector{"test-node": mockCollector},
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
	node := &config.Node{
		Name: "test-node",
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
	}

	event := rs.processAccount(node, account, mockCollector)

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
	rs := &RefundScheduler{
		collectors:       map[string]collector.IModuleCollector{"test-node": mockCollector},
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
	node := &config.Node{
		Name: "test-node",
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
	}

	event := rs.processAccount(node, account, mockCollector)

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

func TestIsRunning(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}

	cfg := &config.Schema{
		Global: config.Global{
			AutoRefund: &config.AutoRefund{
				Enabled:   true,
				FaucetURL: "http://test-faucet",
				Schedule:  "*/30 * * * * *", // Every 30 seconds
				Timeout:   30,
			},
		},
		Nodes: []*config.Node{},
	}

	rs, err := NewRefundScheduler(cfg, currencyRegistry, mockFaucet)
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

func TestStartStop(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}

	cfg := &config.Schema{
		Global: config.Global{
			AutoRefund: &config.AutoRefund{
				Enabled:   true,
				FaucetURL: "http://test-faucet",
				Schedule:  "*/30 * * * * *", // Every 30 seconds
				Timeout:   30,
			},
		},
		Nodes: []*config.Node{},
	}

	rs, err := NewRefundScheduler(cfg, currencyRegistry, mockFaucet)
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

func TestGetNextRun(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}

	cfg := &config.Schema{
		Global: config.Global{
			AutoRefund: &config.AutoRefund{
				Enabled:   true,
				FaucetURL: "http://test-faucet",
				Schedule:  "@every 1m", // Every 1 minute
				Timeout:   30,
			},
		},
		Nodes: []*config.Node{},
	}

	rs, err := NewRefundScheduler(cfg, currencyRegistry, mockFaucet)
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

func TestProcessAccounts(t *testing.T) {
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

	refundThreshold := 5.0
	refundTarget := 10.0
	cfg := &config.Schema{
		Global: config.Global{
			AutoRefund: &config.AutoRefund{
				Enabled:   true,
				FaucetURL: "http://test-faucet",
				Schedule:  "*/30 * * * * *",
				Timeout:   30,
			},
		},
		Nodes: []*config.Node{
			{
				Name: "test-node",
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
						Address:         "0x1234567890123456789012345678901234567890",
						Name:            "test-account-1",
						RefundThreshold: &refundThreshold,
						RefundTarget:    &refundTarget,
					},
					{
						Address:         "0x1234567890123456789012345678901234567891",
						Name:            "test-account-2",
						RefundThreshold: &refundThreshold,
						RefundTarget:    &refundTarget,
					},
				},
			},
		},
	}

	rs := &RefundScheduler{
		config:           cfg,
		collectors:       map[string]collector.IModuleCollector{"test-node": mockCollector},
		currencyRegistry: currencyRegistry,
		faucetClient:     mockFaucet,
		ctx:              context.Background(),
		eventBufferSize:  MAX_BUFFER_SIZE,
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

func TestNewRefundScheduler(t *testing.T) {
	currencyRegistry := currency.NewDefaultRegistry()
	mockFaucet := &mockFauceter{}

	t.Run("AutoRefundDisabled", func(t *testing.T) {
		cfg := &config.Schema{
			Global: config.Global{
				AutoRefund: &config.AutoRefund{
					Enabled: false,
				},
			},
		}
		_, err := NewRefundScheduler(cfg, currencyRegistry, mockFaucet)
		if err == nil {
			t.Error("Expected error when auto-refund is disabled, but got nil")
		}
		expectedErr := "auto-refund is not enabled in configuration"
		if err.Error() != expectedErr {
			t.Errorf("Expected error message '%s', but got '%s'", expectedErr, err.Error())
		}
	})

	t.Run("NoFaucetURL", func(t *testing.T) {
		cfg := &config.Schema{
			Global: config.Global{
				AutoRefund: &config.AutoRefund{
					Enabled:   true,
					FaucetURL: "",
				},
			},
		}
		_, err := NewRefundScheduler(cfg, currencyRegistry, mockFaucet)
		if err == nil {
			t.Error("Expected error when faucet URL is missing, but got nil")
		}
		expectedErr := "faucet URL is required for auto-refund"
		if err.Error() != expectedErr {
			t.Errorf("Expected error message '%s', but got '%s'", expectedErr, err.Error())
		}
	})

	t.Run("ValidConfiguration", func(t *testing.T) {
		cfg := &config.Schema{
			Global: config.Global{
				AutoRefund: &config.AutoRefund{
					Enabled:   true,
					FaucetURL: "http://test-faucet",
					Schedule:  "@every 1m",
					Timeout:   30,
				},
			},
			Nodes: []*config.Node{},
		}
		rs, err := NewRefundScheduler(cfg, currencyRegistry, mockFaucet)
		if err != nil {
			t.Errorf("Unexpected error for valid configuration: %v", err)
		}
		if rs == nil {
			t.Error("Expected scheduler to be created, but got nil")
		}
	})
}
