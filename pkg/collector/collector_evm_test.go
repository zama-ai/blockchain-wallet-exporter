package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

// mockEVMServer creates a test server that simulates an Ethereum node
func mockEVMServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify content type
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}

		var req struct {
			JsonRPC string        `json:"jsonrpc"`
			Method  string        `json:"method"`
			Params  []interface{} `json:"params"`
			ID      int           `json:"id"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)

		var response interface{}
		switch req.Method {
		case "eth_getBalance":
			// Return a mock balance of 1 ETH (in wei)
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  "0xDE0B6B3A7640000", // 1 ETH in wei (1000000000000000000)
			}
		default:
			t.Fatalf("unexpected RPC method: %s", req.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
}

func init() {
	_ = logger.InitLogger()
}

func TestEVMCollector_CollectAccountBalance(t *testing.T) {
	// Create mock server
	server := mockEVMServer(t)
	defer server.Close()

	// Setup test currency registry
	currencyRegistry := currency.NewRegistry()
	currencyRegistry.MustRegister(currency.DefaultETH)
	currencyRegistry.MustRegister(currency.DefaultWEI)

	// Test cases
	tests := []struct {
		name           string
		nodeConfig     config.Node
		account        *config.Account
		labels         prometheus.Labels
		expectedValue  float64
		expectedHealth float64
		expectError    bool
	}{
		{
			name: "successful balance query in eth",
			nodeConfig: config.Node{
				Name:          "test-eth-node",
				HttpAddr:      server.URL,
				Module:        "evm",
				Unit:          currencyRegistry.MustGet("ETH"),
				HttpSSLVerify: "true",
				Authorization: nil,
				Labels:        prometheus.Labels{"app": "test-eth-node"},
			},
			account: &config.Account{
				Name:    "test-account",
				Address: "0x742d35Cc6634C0532925a3b844Bc454e4438f44e",
			},
			expectedValue:  1.0, // 1 ETH
			expectedHealth: 1.0,
			expectError:    false,
		},
		{
			name: "successful balance query in wei",
			nodeConfig: config.Node{
				Name:          "test-eth-node",
				HttpAddr:      server.URL,
				Module:        "evm",
				Unit:          currencyRegistry.MustGet("WEI"),
				HttpSSLVerify: "true",
				Authorization: nil,
				Labels:        prometheus.Labels{"app": "test-eth-node"},
			},
			account: &config.Account{
				Name:    "test-account",
				Address: "0x742d35Cc6634C0532925a3b844Bc454e4438f44e",
			},
			expectedValue:  1000000000000000000.0, // 1 ETH in wei
			expectedHealth: 1.0,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewEVMCollector(
				tt.nodeConfig,
				currencyRegistry,
			)
			assert.NoError(t, err)

			// Ensure collector is of the correct type
			baseCollector, ok := collector.(*BaseCollector)
			if !ok {
				t.Fatalf("collector is not of type *BaseCollector")
			}
			defer baseCollector.Close()

			result, err := baseCollector.processor.CollectAccountBalance(context.Background(), tt.account)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedValue, result.Value)
			assert.Equal(t, tt.expectedHealth, result.Health)
			assert.Equal(t, tt.nodeConfig.Name, result.NodeName)
			assert.Equal(t, *tt.account, result.Account)
		})
	}
}
