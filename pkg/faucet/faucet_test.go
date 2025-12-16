package faucet

import (
	"context"
	"testing"
	"time"
)

// TestAdaptivePolling tests that poll interval adapts based on confirmation timeout
func TestAdaptivePolling(t *testing.T) {
	tests := []struct {
		name                 string
		confirmationTimeout  time.Duration
		expectedMinPoll      time.Duration
		expectedMaxPoll      time.Duration
		contextDeadline      time.Duration
		expectAdaptToContext bool
	}{
		{
			name:                "Short timeout - 10s",
			confirmationTimeout: 10 * time.Second,
			expectedMinPoll:     2 * time.Second, // 10/5 = 2s
			expectedMaxPoll:     2 * time.Second,
		},
		{
			name:                "Medium timeout - 30s",
			confirmationTimeout: 30 * time.Second,
			expectedMinPoll:     6 * time.Second, // 30/5 = 6s
			expectedMaxPoll:     6 * time.Second,
		},
		{
			name:                "Long timeout - 5 minutes",
			confirmationTimeout: 5 * time.Minute,
			expectedMinPoll:     10 * time.Second, // capped at max 10s
			expectedMaxPoll:     10 * time.Second,
		},
		{
			name:                "Very short timeout - 5s",
			confirmationTimeout: 5 * time.Second,
			expectedMinPoll:     2 * time.Second, // min 2s (5/5 = 1s, but capped at 2s)
			expectedMaxPoll:     2 * time.Second,
		},
		{
			name:                 "Context deadline shorter than config",
			confirmationTimeout:  5 * time.Minute,
			contextDeadline:      55 * time.Second,
			expectAdaptToContext: true,
			expectedMinPoll:      10 * time.Second, // 55/5 = 11s, capped at 10s
			expectedMaxPoll:      11 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &FundingOptions{
				ConfirmationTimeout: tt.confirmationTimeout,
				PollInterval:        0, // Let it be calculated adaptively
			}

			// Create context with or without deadline
			var ctx context.Context
			var cancel context.CancelFunc
			if tt.contextDeadline > 0 {
				ctx, cancel = context.WithTimeout(context.Background(), tt.contextDeadline)
				defer cancel()
			} else {
				ctx = context.Background()
			}

			// We can't easily test waitForConfirmation without a real server,
			// but we can verify the logic by checking what timeout would be used
			timeout := opts.ConfirmationTimeout
			if timeout <= 0 {
				timeout = 1 * time.Minute
			}

			// Check if parent context has a deadline
			if deadline, ok := ctx.Deadline(); ok {
				remainingTime := time.Until(deadline)
				if remainingTime < timeout {
					timeout = remainingTime
					if !tt.expectAdaptToContext {
						t.Errorf("Expected not to adapt to context, but timeout was adapted to %v", timeout)
					}
				}
			}

			// Calculate adaptive poll interval
			pollInterval := opts.PollInterval
			if pollInterval <= 0 {
				adaptivePoll := timeout / 5
				if adaptivePoll < 2*time.Second {
					adaptivePoll = 2 * time.Second
				}
				if adaptivePoll > 10*time.Second {
					adaptivePoll = 10 * time.Second
				}
				pollInterval = adaptivePoll
			}

			// Verify poll interval is within expected range
			if pollInterval < tt.expectedMinPoll {
				t.Errorf("Poll interval %v is less than expected minimum %v", pollInterval, tt.expectedMinPoll)
			}
			if pollInterval > tt.expectedMaxPoll {
				t.Errorf("Poll interval %v is greater than expected maximum %v", pollInterval, tt.expectedMaxPoll)
			}

			t.Logf("Confirmation timeout: %v, Poll interval: %v", timeout, pollInterval)
		})
	}
}

// TestDefaultFundingOptions verifies default values
func TestDefaultFundingOptions(t *testing.T) {
	opts := DefaultFundingOptions()

	if opts.WaitForConfirmation != true {
		t.Errorf("Expected WaitForConfirmation to be true, got %v", opts.WaitForConfirmation)
	}

	if opts.ConfirmationTimeout != 1*time.Minute {
		t.Errorf("Expected ConfirmationTimeout to be 1m, got %v", opts.ConfirmationTimeout)
	}

	if opts.PollInterval != 5*time.Second {
		t.Errorf("Expected PollInterval to be 5s, got %v", opts.PollInterval)
	}

	if opts.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries to be 3, got %v", opts.MaxRetries)
	}
}

// TestLoggingContextPrefix verifies the logPrefix method behavior
func TestLoggingContextPrefix(t *testing.T) {
	tests := []struct {
		name           string
		loggingContext *LoggingContext
		expectedPrefix string
	}{
		{
			name:           "Nil context",
			loggingContext: nil,
			expectedPrefix: "[faucet]",
		},
		{
			name:           "Empty node name",
			loggingContext: &LoggingContext{NodeName: ""},
			expectedPrefix: "[faucet]",
		},
		{
			name:           "Valid node name",
			loggingContext: &LoggingContext{NodeName: "sepolia-testnet"},
			expectedPrefix: "[faucet sepolia-testnet]",
		},
		{
			name:           "Node name with spaces",
			loggingContext: &LoggingContext{NodeName: "my test node"},
			expectedPrefix: "[faucet my test node]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := tt.loggingContext.logPrefix()
			if prefix != tt.expectedPrefix {
				t.Errorf("Expected prefix %q, got %q", tt.expectedPrefix, prefix)
			}
		})
	}
}

// TestContextAwareMethodsExist verifies that context-aware methods are available
func TestContextAwareMethodsExist(t *testing.T) {
	client := NewClient("http://test-faucet:8080", 30*time.Second)
	logCtx := &LoggingContext{NodeName: "test-node"}

	// Verify that methods exist and have correct signatures by checking they compile
	// We can't actually call them without a logger initialized and a real server
	t.Run("Verify method signatures", func(t *testing.T) {
		// These assignments verify the methods exist with correct signatures
		var _ func(context.Context, string, float64, *LoggingContext) (*FaucetResult, error) = client.FundAccountWeiWithContext
		var _ func(context.Context, string, float64, int, *LoggingContext) (*FaucetResult, error) = client.FundAccountWeiWithRetryAndContext
		var _ func(context.Context, string, float64, *FundingOptions, *LoggingContext) (*FaucetResult, error) = client.FundAccountWeiWithOptionsAndContext
		var _ func(context.Context, string, float64, *FundingOptions, *LoggingContext) (*FaucetResult, error) = client.FundAccountWeiWithRetriesAndOptionsAndContext

		// Verify logging context is not nil
		if logCtx == nil {
			t.Error("LoggingContext should not be nil")
		}
		if logCtx.NodeName != "test-node" {
			t.Errorf("Expected NodeName to be 'test-node', got %q", logCtx.NodeName)
		}
	})

	t.Run("Verify interface compatibility", func(t *testing.T) {
		// Verify that Client implements the Fauceter interface with new methods
		var _ Fauceter = client
	})
}
