package config

import (
	"strings"
	"testing"
)

func TestCurrencyUnitYAMLParsing(t *testing.T) {
	yamlContent := `
global:
  environment: "dev"
  metricsAddr: ":2112"
  logLevel: "info"
nodes:
  - name: "ethereum-node"
    module: "evm"
    httpAddr: "http://localhost:8545"
    unit: "ETH"
    accounts:
      - address: "0x123"
        name: "test-account"
  - name: "polygon-node"
    module: "evm"
    httpAddr: "http://localhost:8546"
    unit: "wei"
    accounts:
      - address: "0x456"
        name: "test-account-2"
`

	config, err := ReadConfigWithError(strings.NewReader(yamlContent))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	tests := []struct {
		nodeName    string
		wantUnit    string
		wantUnitStr string
	}{
		{"ethereum-node", "eth", "eth"},
		{"polygon-node", "wei", "wei"},
	}

	for _, tt := range tests {
		t.Run(tt.nodeName, func(t *testing.T) {
			var node *Node
			for _, n := range config.Nodes {
				if n.Name == tt.nodeName {
					node = n
					break
				}
			}
			if node == nil {
				t.Fatalf("node %s not found", tt.nodeName)
			}

			if node.Unit.Name != tt.wantUnit {
				t.Errorf("got unit %v, want %v", node.Unit.Name, tt.wantUnit)
			}

			if node.Unit.Symbol != tt.wantUnitStr {
				t.Errorf("got unit string %v, want %v", node.Unit.Symbol, tt.wantUnitStr)
			}
		})
	}
}

func TestAddressEnvOverride(t *testing.T) {
	// Set environment variables for the test
	const envVarName = "TEST_ACCOUNT_3_ADDRESS"
	const envVarValue = "0xENV_ADDRESS_OVERRIDE"
	t.Setenv(envVarName, envVarValue)

	yamlContent := `
global:
  environment: "test"
  metricsAddr: ":2113"
  logLevel: "debug"
nodes:
  - name: "test-node"
    module: "evm"
    httpAddr: "http://localhost:8545"
    unit: "ETH"
    accounts:
      - address: "0xYAML_ADDRESS_1"
        name: "test-account-1" # No addressEnv
      - address: "0xYAML_ADDRESS_2"
        name: "test-account-2"
        addressEnv: "NON_EXISTENT_ENV_VAR" # Env var not set
      - address: "0xYAML_ADDRESS_3"
        name: "test-account-3"
        addressEnv: "TEST_ACCOUNT_3_ADDRESS" # Env var is set
      - name: "test-account-4" # No initial address
        addressEnv: "TEST_ACCOUNT_3_ADDRESS" # Env var is set
`

	config, err := ReadConfigWithError(strings.NewReader(yamlContent))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	if len(config.Nodes) != 1 || len(config.Nodes[0].Accounts) != 4 {
		t.Fatalf("unexpected number of nodes or accounts parsed")
	}

	accounts := config.Nodes[0].Accounts

	tests := []struct {
		accountName string
		wantAddress string
	}{
		{"test-account-1", "0xYAML_ADDRESS_1"},
		{"test-account-2", "0xYAML_ADDRESS_2"}, // Env var not set, should keep YAML address
		{"test-account-3", envVarValue},        // Env var set, should use env address
		{"test-account-4", envVarValue},        // No YAML address, env var set
	}

	accountMap := make(map[string]*Account)
	for _, acc := range accounts {
		accountMap[acc.Name] = acc
	}

	for _, tt := range tests {
		t.Run(tt.accountName, func(t *testing.T) {
			acc, ok := accountMap[tt.accountName]
			if !ok {
				t.Fatalf("account %s not found in parsed config", tt.accountName)
			}
			if acc.Address != tt.wantAddress {
				t.Errorf("got address %q, want %q", acc.Address, tt.wantAddress)
			}
		})
	}
}
