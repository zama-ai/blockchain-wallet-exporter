package faucet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

// Client represents a faucet client for funding accounts
type Client struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// LoggingContext provides context information for enhanced logging
type LoggingContext struct {
	NodeName string
	BaseUnit string // The base unit being used (e.g., "wei", "utoken", etc.)
}

// logPrefix returns a consistent log prefix for faucet operations
func (lc *LoggingContext) logPrefix() string {
	if lc == nil || lc.NodeName == "" {
		return "[faucet]"
	}
	return fmt.Sprintf("[faucet %s]", lc.NodeName)
}

// StartSessionRequest represents the request payload for starting a session
type StartSessionRequest struct {
	Address string  `json:"addr"`
	Amount  float64 `json:"amount"` // Amount in base unit (wei for ETH, smallest unit for ERC20, etc.)
}

// StartSessionResponse represents the response from starting a session
type StartSessionResponse struct {
	Session string         `json:"session"`
	Status  string         `json:"status"`
	Start   int64          `json:"start"`
	Tasks   []any          `json:"tasks"`
	Balance string         `json:"balance"`
	Target  string         `json:"target"`
	Modules map[string]any `json:"modules"`
}

// ClaimRewardRequest represents the request payload for claiming a reward
type ClaimRewardRequest struct {
	Session string `json:"session"`
}

// ClaimRewardResponse represents the response from claiming a reward
type ClaimRewardResponse struct {
	Session     string         `json:"session"`
	Status      string         `json:"status"`
	Start       int64          `json:"start"`
	Tasks       []any          `json:"tasks"`
	Balance     string         `json:"balance"`
	Target      string         `json:"target"`
	ClaimIdx    *int           `json:"claimIdx,omitempty"`
	ClaimStatus *string        `json:"claimStatus,omitempty"`
	Modules     map[string]any `json:"modules"`
}

// SessionStatusResponse represents the response from checking session status
type SessionStatusResponse struct {
	Session     string         `json:"session"`
	Status      string         `json:"status"`
	Start       int64          `json:"start"`
	Tasks       []any          `json:"tasks"`
	Balance     string         `json:"balance"`
	Target      string         `json:"target"`
	ClaimIdx    *int           `json:"claimIdx,omitempty"`
	ClaimStatus *string        `json:"claimStatus,omitempty"`
	ClaimBlock  *int64         `json:"claimBlock,omitempty"`
	ClaimHash   *string        `json:"claimHash,omitempty"`
	Modules     map[string]any `json:"modules"`
}

// FaucetResult represents the result of a faucet operation
type FaucetResult struct {
	Address        string
	AmountBaseUnit float64 // Amount in base unit (wei for ETH, smallest unit for ERC20, etc.)
	BaseUnit       string  // The base unit used (e.g., "wei", "utoken", etc.)
	Session        string
	Status         string
	ClaimStatus    string
	ClaimBlock     int64
	ClaimHash      string
	Balance        string
	Target         string
	Success        bool
	Confirmed      bool
	Error          error
	Duration       time.Duration
}

// FundingOptions represents options for funding operations
type FundingOptions struct {
	WaitForConfirmation bool
	ConfirmationTimeout time.Duration
	PollInterval        time.Duration
	MaxRetries          int
}

// DefaultFundingOptions returns sensible defaults for funding operations
func DefaultFundingOptions() *FundingOptions {
	return &FundingOptions{
		WaitForConfirmation: true, // Wait for on-chain confirmation by default for safety
		ConfirmationTimeout: 1 * time.Minute,
		PollInterval:        5 * time.Second,
		MaxRetries:          3,
	}
}

// NewClient creates a new faucet client
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// FundAccount funds an account with an amount specified in base unit
func (c *Client) FundAccount(ctx context.Context, address string, amountBaseUnit float64) (*FaucetResult, error) {
	return c.FundAccountWithOptions(ctx, address, amountBaseUnit, DefaultFundingOptions())
}

// FundAccountWithContext funds an account with an amount specified in base unit and logging context
func (c *Client) FundAccountWithContext(ctx context.Context, address string, amountBaseUnit float64, logCtx *LoggingContext) (*FaucetResult, error) {
	return c.FundAccountWithOptionsAndContext(ctx, address, amountBaseUnit, DefaultFundingOptions(), logCtx)
}

// FundAccountWithOptions funds an account with base unit amount and custom options
func (c *Client) FundAccountWithOptions(ctx context.Context, address string, amountBaseUnit float64, opts *FundingOptions) (*FaucetResult, error) {
	return c.FundAccountWithOptionsAndContext(ctx, address, amountBaseUnit, opts, nil)
}

// FundAccountWithOptionsAndContext funds an account with base unit amount, custom options, and logging context
func (c *Client) FundAccountWithOptionsAndContext(ctx context.Context, address string, amountBaseUnit float64, opts *FundingOptions, logCtx *LoggingContext) (*FaucetResult, error) {
	startTime := time.Now()

	result := &FaucetResult{
		Address:        address,
		AmountBaseUnit: amountBaseUnit,
		BaseUnit:       "",
	}

	// Set base unit from logging context if available
	if logCtx != nil && logCtx.BaseUnit != "" {
		result.BaseUnit = logCtx.BaseUnit
	}

	prefix := ""
	if logCtx != nil {
		prefix = logCtx.logPrefix() + " "
	}

	// Determine unit string for logging
	unitStr := "base units"
	if logCtx != nil && logCtx.BaseUnit != "" {
		unitStr = logCtx.BaseUnit
	}

	logger.Infof("%sStarting faucet funding for address %s with amount %.0f %s", prefix, address, amountBaseUnit, unitStr)

	// Step 1: Start session (send amount in base unit)
	sessionResp, err := c.startSession(ctx, address, amountBaseUnit)
	if err != nil {
		result.Error = fmt.Errorf("failed to start session: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Session = sessionResp.Session
	result.Status = sessionResp.Status
	result.Balance = sessionResp.Balance
	result.Target = sessionResp.Target

	logger.Debugf("%sStarted faucet session %s for address %s, status: %s", prefix, sessionResp.Session, address, sessionResp.Status)

	// Check if session is claimable
	if sessionResp.Status != "claimable" {
		result.Error = fmt.Errorf("session not claimable, status: %s", sessionResp.Status)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	// Step 2: Claim reward
	claimResult, err := c.claimReward(ctx, sessionResp.Session)
	if err != nil {
		result.Error = fmt.Errorf("failed to claim reward: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Status = claimResult.Status
	result.ClaimStatus = "<nil>"
	if claimResult.ClaimStatus != nil {
		result.ClaimStatus = *claimResult.ClaimStatus
	}

	// Handle different claim states
	switch {
	// Case 1: Claim is queued or being processed
	case claimResult.Status == "claiming" && claimResult.ClaimStatus != nil && result.ClaimStatus == "queue":
		if opts.WaitForConfirmation {
			// Wait for confirmation before marking as success
			confirmed, err := c.waitForConfirmation(ctx, sessionResp.Session, opts, logCtx)
			if err != nil {
				result.Error = fmt.Errorf("failed to confirm transaction: %w", err)
				result.Duration = time.Since(startTime)
				return result, result.Error
			}

			// Successfully confirmed
			result.Success = true
			result.Confirmed = true
			result.Status = confirmed.Status
			if confirmed.ClaimStatus != nil {
				result.ClaimStatus = *confirmed.ClaimStatus
			}
			if confirmed.ClaimBlock != nil {
				result.ClaimBlock = *confirmed.ClaimBlock
			}
			if confirmed.ClaimHash != nil {
				result.ClaimHash = *confirmed.ClaimHash
			}
		} else {
			// Not waiting for confirmation, just queued is considered success
			result.Success = true
		}

	// Case 2: Claim is already finished (immediate confirmation)
	case claimResult.Status == "finished" && claimResult.ClaimStatus != nil:
		switch result.ClaimStatus {
		case "confirmed":
			result.Success = true
			result.Confirmed = true
			if claimResult.ClaimIdx != nil {
				// Try to get full status with transaction details
				if status, err := c.GetSessionStatus(ctx, sessionResp.Session); err == nil {
					if status.ClaimHash != nil {
						result.ClaimHash = *status.ClaimHash
					}
					if status.ClaimBlock != nil {
						result.ClaimBlock = *status.ClaimBlock
					}
				}
			}
		case "failed", "error":
			result.Error = fmt.Errorf("claim failed with status: %s", *claimResult.ClaimStatus)
			result.Duration = time.Since(startTime)
			return result, result.Error
		default:
			result.Error = fmt.Errorf("unexpected finished claim status: %s", *claimResult.ClaimStatus)
			result.Duration = time.Since(startTime)
			return result, result.Error
		}

	// Case 3: Unexpected status
	default:
		result.Error = fmt.Errorf("unexpected claim status: %s, claim status: %s", claimResult.Status, result.ClaimStatus)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Duration = time.Since(startTime)

	// Log appropriate message based on confirmation status
	if result.Confirmed {
		// Transaction confirmed - log with or without hash
		if result.ClaimHash != "" {
			logger.Infof("%sSuccessfully confirmed faucet claim for address %s with amount %.0f %s, session: %s, tx: %s, duration: %v",
				prefix, address, amountBaseUnit, unitStr, sessionResp.Session, result.ClaimHash, result.Duration)
		} else {
			logger.Infof("%sSuccessfully confirmed faucet claim for address %s with amount %.0f %s, session: %s, duration: %v (tx hash not available)",
				prefix, address, amountBaseUnit, unitStr, sessionResp.Session, result.Duration)
		}
	} else if result.Success {
		// Only logged when WaitForConfirmation = false
		logger.Infof("%sQueued faucet claim for address %s with amount %.0f %s, session: %s, status: %s, duration: %v (not waiting for confirmation)",
			prefix, address, amountBaseUnit, unitStr, sessionResp.Session, result.ClaimStatus, result.Duration)
	}

	return result, nil
}

// FundAccountWithConfirmation funds an account with base unit amount and waits for confirmation
func (c *Client) FundAccountWithConfirmation(ctx context.Context, address string, amountBaseUnit float64) (*FaucetResult, error) {
	opts := DefaultFundingOptions()
	opts.WaitForConfirmation = true
	return c.FundAccountWithOptions(ctx, address, amountBaseUnit, opts)
}

// GetSessionStatus retrieves the current status of a session
func (c *Client) GetSessionStatus(ctx context.Context, session string) (*SessionStatusResponse, error) {
	url := fmt.Sprintf("%s/api/getSessionStatus?session=%s", c.baseURL, session)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var statusResp SessionStatusResponse
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &statusResp, nil
}

// waitForConfirmation polls the session status until it's confirmed or timeout
func (c *Client) waitForConfirmation(ctx context.Context, session string, opts *FundingOptions, logCtx *LoggingContext) (*SessionStatusResponse, error) {
	prefix := ""
	if logCtx != nil {
		prefix = logCtx.logPrefix() + " "
	}
	logger.Debugf("%sWaiting for confirmation of session %s", prefix, session)

	// Determine effective timeout: use parent context deadline if available, else use configured timeout
	timeout := opts.ConfirmationTimeout
	if timeout <= 0 {
		timeout = 1 * time.Minute
	}

	// Check if parent context has a deadline and use the shorter of the two
	if deadline, ok := ctx.Deadline(); ok {
		remainingTime := time.Until(deadline)
		if remainingTime < timeout {
			timeout = remainingTime
			logger.Debugf("%sAdapting confirmation timeout to parent context deadline: %v", prefix, timeout)
		}
	}

	// Adaptive poll interval: 20% of timeout, min 2s, max 10s
	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		// Calculate adaptive poll interval
		adaptivePoll := timeout / 5
		if adaptivePoll < 2*time.Second {
			adaptivePoll = 2 * time.Second
		}
		if adaptivePoll > 10*time.Second {
			adaptivePoll = 10 * time.Second
		}
		pollInterval = adaptivePoll
		logger.Debugf("%sUsing adaptive poll interval: %v (timeout: %v)", prefix, pollInterval, timeout)
	}

	confirmCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-confirmCtx.Done():
			return nil, fmt.Errorf("timeout waiting for confirmation: %w", confirmCtx.Err())
		case <-ticker.C:
			status, err := c.GetSessionStatus(confirmCtx, session)
			if err != nil {
				logger.Warnf("%sFailed to get session status for %s: %v", prefix, session, err)
				continue
			}

			// Safely dereference ClaimStatus pointer for logging
			claimStatusStr := "<nil>"
			if status.ClaimStatus != nil {
				claimStatusStr = *status.ClaimStatus
			}
			logger.Infof("%sSession %s status: %s, claim status: %s", prefix, session, status.Status, claimStatusStr)

			// Check for completion states
			if status.Status == "finished" {
				if status.ClaimStatus == nil {
					return nil, fmt.Errorf("session finished but claim status is nil")
				}
				switch *status.ClaimStatus {
				case "confirmed":
					// Safely dereference ClaimHash pointer for logging
					claimHashStr := "<nil>"
					if status.ClaimHash != nil {
						claimHashStr = *status.ClaimHash
					}
					logger.Infof("%sSession %s confirmed with hash %s", prefix, session, claimHashStr)
					return status, nil
				case "failed", "error":
					return nil, fmt.Errorf("session claim failed with ClaimStatus: %s and Status: %s", *status.ClaimStatus, status.Status)
				default:
					logger.Warnf("%sSession %s has finished with unexpected claim status: %s, continuing to poll", prefix, session, *status.ClaimStatus)
				}
			}

			// Continue polling for in-progress states (claiming/queue, claiming/processing, etc.)
		}
	}
}

// startSession starts a faucet session
func (c *Client) startSession(ctx context.Context, address string, amountBaseUnit float64) (*StartSessionResponse, error) {
	url := fmt.Sprintf("%s/api/startSession", c.baseURL)

	reqBody := StartSessionRequest{
		Address: address,
		Amount:  amountBaseUnit, // Amount in base unit (wei for ETH, smallest unit for ERC20, etc.)
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var sessionResp StartSessionResponse
	if err := json.Unmarshal(body, &sessionResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if sessionResp.Session == "" {
		return nil, fmt.Errorf("empty session ID received")
	}

	return &sessionResp, nil
}

// claimReward claims the reward for a session
func (c *Client) claimReward(ctx context.Context, session string) (*ClaimRewardResponse, error) {
	url := fmt.Sprintf("%s/api/claimReward", c.baseURL)

	reqBody := ClaimRewardRequest{
		Session: session,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var claimResp ClaimRewardResponse
	if err := json.Unmarshal(body, &claimResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &claimResp, nil
}

// FundAccountWithRetry funds an account with base unit amount and retry logic
func (c *Client) FundAccountWithRetry(ctx context.Context, address string, amountBaseUnit float64, maxRetries int) (*FaucetResult, error) {
	opts := DefaultFundingOptions()
	opts.MaxRetries = maxRetries
	return c.FundAccountWithRetriesAndOptions(ctx, address, amountBaseUnit, opts)
}

// FundAccountWithRetryAndContext funds an account with base unit amount, retry logic, and logging context
func (c *Client) FundAccountWithRetryAndContext(ctx context.Context, address string, amountBaseUnit float64, maxRetries int, logCtx *LoggingContext) (*FaucetResult, error) {
	opts := DefaultFundingOptions()
	opts.MaxRetries = maxRetries
	return c.FundAccountWithRetriesAndOptionsAndContext(ctx, address, amountBaseUnit, opts, logCtx)
}

// FundAccountWithRetriesAndOptions funds an account with base unit amount, retry logic and custom options
func (c *Client) FundAccountWithRetriesAndOptions(ctx context.Context, address string, amountBaseUnit float64, opts *FundingOptions) (*FaucetResult, error) {
	return c.FundAccountWithRetriesAndOptionsAndContext(ctx, address, amountBaseUnit, opts, nil)
}

// FundAccountWithRetriesAndOptionsAndContext funds an account with base unit amount, retry logic, custom options, and logging context
func (c *Client) FundAccountWithRetriesAndOptionsAndContext(ctx context.Context, address string, amountBaseUnit float64, opts *FundingOptions, logCtx *LoggingContext) (*FaucetResult, error) {
	var lastResult *FaucetResult
	var lastErr error

	prefix := ""
	if logCtx != nil {
		prefix = logCtx.logPrefix() + " "
	}

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	// Total attempts = 1 initial + maxRetries retries
	totalAttempts := maxRetries + 1
	for attempt := 0; attempt < totalAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt*attempt) * time.Second
			logger.Warnf("%sRetrying faucet funding for %s (attempt %d/%d) after %v", prefix, address, attempt+1, totalAttempts, backoff)

			select {
			case <-ctx.Done():
				return lastResult, ctx.Err()
			case <-time.After(backoff):
			}
		}

		result, err := c.FundAccountWithOptionsAndContext(ctx, address, amountBaseUnit, opts, logCtx)
		if err == nil {
			return result, nil
		}

		lastResult = result
		lastErr = err
		logger.Warnf("%sFaucet funding attempt %d failed for %s: %v", prefix, attempt+1, address, err)
	}

	return lastResult, fmt.Errorf("failed after %d attempts: %w", totalAttempts, lastErr)
}
