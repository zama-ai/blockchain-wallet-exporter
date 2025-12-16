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

// StartSessionRequest represents the request payload for starting a session
type StartSessionRequest struct {
	Address string  `json:"addr"`
	Amount  float64 `json:"amount"` // Amount in wei
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
	Address     string
	AmountWei   float64 // Amount in wei (only unit supported)
	Session     string
	Status      string
	ClaimStatus string
	ClaimBlock  int64
	ClaimHash   string
	Balance     string
	Target      string
	Success     bool
	Confirmed   bool
	Error       error
	Duration    time.Duration
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
		ConfirmationTimeout: 5 * time.Minute,
		PollInterval:        10 * time.Second,
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

// FundAccountWei funds an account with an amount specified in wei
func (c *Client) FundAccountWei(ctx context.Context, address string, amountWei float64) (*FaucetResult, error) {
	return c.FundAccountWeiWithOptions(ctx, address, amountWei, DefaultFundingOptions())
}

// FundAccountWeiWithOptions funds an account with wei amount and custom options
func (c *Client) FundAccountWeiWithOptions(ctx context.Context, address string, amountWei float64, opts *FundingOptions) (*FaucetResult, error) {
	startTime := time.Now()

	result := &FaucetResult{
		Address:   address,
		AmountWei: amountWei,
	}

	logger.Infof("Starting faucet funding for address %s with amount %.0f wei", address, amountWei)

	// Step 1: Start session (send amount in wei)
	sessionResp, err := c.startSession(ctx, address, amountWei)
	if err != nil {
		result.Error = fmt.Errorf("failed to start session: %w", err)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Session = sessionResp.Session
	result.Status = sessionResp.Status
	result.Balance = sessionResp.Balance
	result.Target = sessionResp.Target

	logger.Debugf("Started faucet session %s for address %s, status: %s", sessionResp.Session, address, sessionResp.Status)

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
	if claimResult.ClaimStatus != nil {
		result.ClaimStatus = *claimResult.ClaimStatus
	}

	// Handle different claim states
	switch {
	// Case 1: Claim is queued or being processed
	case claimResult.Status == "claiming" && claimResult.ClaimStatus != nil && *claimResult.ClaimStatus == "queue":
		result.Success = true // Queued for processing (not yet confirmed)

		if opts.WaitForConfirmation {
			// Wait for confirmation
			confirmed, err := c.waitForConfirmation(ctx, sessionResp.Session, opts)
			if err != nil {
				logger.Warnf("Failed to wait for confirmation for session %s: %v", sessionResp.Session, err)
				// Don't fail the entire operation, just log the warning
			} else if confirmed != nil {
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
			}
		}

	// Case 2: Claim is already finished (immediate confirmation)
	case claimResult.Status == "finished" && claimResult.ClaimStatus != nil:
		switch *claimResult.ClaimStatus {
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
		result.Error = fmt.Errorf("unexpected claim status: %s, claim status: %v", claimResult.Status, claimResult.ClaimStatus)
		result.Duration = time.Since(startTime)
		return result, result.Error
	}

	result.Duration = time.Since(startTime)

	// Log appropriate message
	var logMsg string
	if result.Confirmed && result.ClaimHash != "" {
		logMsg = fmt.Sprintf("Successfully confirmed faucet claim for address %s with amount %.0f wei, session: %s, tx: %s, duration: %v",
			address, amountWei, sessionResp.Session, result.ClaimHash, result.Duration)
	} else {
		logMsg = fmt.Sprintf("Queued faucet claim for address %s with amount %.0f wei, session: %s, status: %s, duration: %v",
			address, amountWei, sessionResp.Session, result.ClaimStatus, result.Duration)
	}

	logger.Infof(logMsg)

	return result, nil
}

// FundAccountWeiWithConfirmation funds an account with wei amount and waits for confirmation
func (c *Client) FundAccountWeiWithConfirmation(ctx context.Context, address string, amountWei float64) (*FaucetResult, error) {
	opts := DefaultFundingOptions()
	opts.WaitForConfirmation = true
	return c.FundAccountWeiWithOptions(ctx, address, amountWei, opts)
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
func (c *Client) waitForConfirmation(ctx context.Context, session string, opts *FundingOptions) (*SessionStatusResponse, error) {
	logger.Debugf("Waiting for confirmation of session %s", session)

	timeout := opts.ConfirmationTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
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
				logger.Warnf("Failed to get session status for %s: %v", session, err)
				continue
			}

			logger.Debugf("Session %s status: %s, claim status: %v", session, status.Status, status.ClaimStatus)

			// Check for completion states
			if status.Status == "finished" {
				if status.ClaimStatus == nil {
					return nil, fmt.Errorf("session finished but claim status is nil")
				}
				switch *status.ClaimStatus {
				case "confirmed":
					logger.Debugf("Session %s confirmed with hash %v", session, status.ClaimHash)
					return status, nil
				case "failed", "error":
					return nil, fmt.Errorf("session claim failed with status: %s", *status.ClaimStatus)
				default:
					logger.Warnf("Session %s has finished with unexpected claim status: %s, continuing to poll", session, *status.ClaimStatus)
				}
			}

			// Continue polling for in-progress states (claiming/queue, claiming/processing, etc.)
		}
	}
}

// startSession starts a faucet session
func (c *Client) startSession(ctx context.Context, address string, amountWei float64) (*StartSessionResponse, error) {
	url := fmt.Sprintf("%s/api/startSession", c.baseURL)

	reqBody := StartSessionRequest{
		Address: address,
		Amount:  amountWei, // Amount in wei
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

// FundAccountWeiWithRetry funds an account with wei amount and retry logic
func (c *Client) FundAccountWeiWithRetry(ctx context.Context, address string, amountWei float64, maxRetries int) (*FaucetResult, error) {
	opts := DefaultFundingOptions()
	opts.MaxRetries = maxRetries
	return c.FundAccountWeiWithRetriesAndOptions(ctx, address, amountWei, opts)
}

// FundAccountWeiWithRetriesAndOptions funds an account with wei amount, retry logic and custom options
func (c *Client) FundAccountWeiWithRetriesAndOptions(ctx context.Context, address string, amountWei float64, opts *FundingOptions) (*FaucetResult, error) {
	var lastResult *FaucetResult
	var lastErr error

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
			logger.Warnf("Retrying faucet funding for %s (attempt %d/%d) after %v", address, attempt+1, totalAttempts, backoff)

			select {
			case <-ctx.Done():
				return lastResult, ctx.Err()
			case <-time.After(backoff):
			}
		}

		result, err := c.FundAccountWeiWithOptions(ctx, address, amountWei, opts)
		if err == nil {
			return result, nil
		}

		lastResult = result
		lastErr = err
		logger.Warnf("Faucet funding attempt %d failed for %s: %v", attempt+1, address, err)
	}

	return lastResult, fmt.Errorf("failed after %d attempts: %w", totalAttempts, lastErr)
}
