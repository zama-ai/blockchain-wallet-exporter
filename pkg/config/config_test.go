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
