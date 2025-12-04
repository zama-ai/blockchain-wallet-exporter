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

func TestHttpAddrEnvOverride(t *testing.T) {
	// Set environment variable for the test
	const envVarName = "TEST_HTTP_ADDR"
	const envVarValue = "https://env-override-rpc.example.com"
	t.Setenv(envVarName, envVarValue)

	yamlContent := `
global:
  environment: "test"
  metricsAddr: ":2113"
  logLevel: "debug"
nodes:
  - name: "node-1"
    module: "evm"
    httpAddr: "https://original-rpc.example.com"
    unit: "ETH"
  - name: "node-2"
    module: "evm"
    httpAddr: "https://original-rpc.example.com"
    httpAddrEnv: "NON_EXISTENT_ENV_VAR"
    unit: "ETH"
  - name: "node-3"
    module: "evm"
    httpAddr: "https://original-rpc.example.com"
    httpAddrEnv: "TEST_HTTP_ADDR"
    unit: "ETH"
  - name: "node-4"
    module: "evm"
    httpAddrEnv: "TEST_HTTP_ADDR"
    unit: "ETH"
`

	config, err := ReadConfigWithError(strings.NewReader(yamlContent))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	if len(config.Nodes) != 4 {
		t.Fatalf("unexpected number of nodes parsed")
	}

	tests := []struct {
		nodeName     string
		wantHttpAddr string
	}{
		{"node-1", "https://original-rpc.example.com"}, // No env var
		{"node-2", "https://original-rpc.example.com"}, // Env var not set
		{"node-3", envVarValue},                        // Env var set, should override
		{"node-4", envVarValue},                        // Only env var, no original
	}

	nodeMap := make(map[string]*Node)
	for _, node := range config.Nodes {
		nodeMap[node.Name] = node
	}

	for _, tt := range tests {
		t.Run(tt.nodeName, func(t *testing.T) {
			node, ok := nodeMap[tt.nodeName]
			if !ok {
				t.Fatalf("node %s not found in parsed config", tt.nodeName)
			}
			if node.HttpAddr != tt.wantHttpAddr {
				t.Errorf("got HttpAddr %q, want %q", node.HttpAddr, tt.wantHttpAddr)
			}
		})
	}
}

func TestERC20AutoUnitDiscoveryParsing(t *testing.T) {
	yamlContent := `
global:
  environment: "dev"
  metricsAddr: ":2112"
  logLevel: "info"
nodes:
  - name: "erc20-node"
    module: "erc20"
    httpAddr: "http://localhost:8545"
    contractAddress: "0x0000000000000000000000000000000000000001"
    autoUnitDiscovery: true
    accounts:
      - address: "0x0000000000000000000000000000000000000002"
        name: "erc20-account"
`

	cfg, err := ReadConfigWithError(strings.NewReader(yamlContent))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	if len(cfg.Nodes) != 1 {
		t.Fatalf("expected 1 node")
	}

	node := cfg.Nodes[0]
	if !node.AutoUnitDiscovery {
		t.Fatalf("expected autoUnitDiscovery to be true")
	}
	if node.Unit != nil || node.MetricsUnit != nil {
		t.Fatalf("expected units to be nil before discovery")
	}
}

func TestERC20NodeParsing(t *testing.T) {
	yamlContent := `
global:
  environment: "dev"
  metricsAddr: ":2112"
  logLevel: "info"
nodes:
  - name: "erc20-node"
    module: "erc20"
    httpAddr: "http://localhost:8545"
    contractAddress: "0x0000000000000000000000000000000000000001"
    unit: "wei"
    metricsUnit: "wei"
    accounts:
      - address: "0x0000000000000000000000000000000000000002"
        name: "erc20-account"
`

	cfg, err := ReadConfigWithError(strings.NewReader(yamlContent))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	if len(cfg.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(cfg.Nodes))
	}

	node := cfg.Nodes[0]
	if !node.IsERC20Module() {
		t.Fatalf("expected node to be recognized as erc20 module")
	}

	if node.ContractAddr != "0x0000000000000000000000000000000000000001" {
		t.Fatalf("unexpected contract address: %s", node.ContractAddr)
	}
}
