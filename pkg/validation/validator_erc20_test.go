package validation

import (
	"testing"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
)

func TestERC20Validator_ValidConfig(t *testing.T) {
	validator := NewERC20Validator()
	node := &config.Node{
		Name:          "erc20-node",
		Module:        "erc20",
		HttpAddr:      "http://localhost:8545",
		ContractAddr:  "0x0000000000000000000000000000000000000001",
		Unit:          &currency.Unit{Name: "wei", Symbol: "wei"},
		MetricsUnit:   &currency.Unit{Name: "wei", Symbol: "wei"},
		Accounts:      []*config.Account{{Address: "0x0000000000000000000000000000000000000002", Name: "acct"}},
		AutoRefund:    &config.AutoRefund{Enabled: true, FaucetURL: "http://faucet", Schedule: "@every 1m", Timeout: 30},
		HttpSSLVerify: "false",
	}

	errs := validator.ValidateNode(node)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestERC20Validator_InvalidContract(t *testing.T) {
	validator := NewERC20Validator()
	node := &config.Node{
		Name:         "erc20-node",
		Module:       "erc20",
		HttpAddr:     "http://localhost:8545",
		ContractAddr: "invalid-address",
		Unit:         &currency.Unit{Name: "wei", Symbol: "wei"},
		MetricsUnit:  &currency.Unit{Name: "wei", Symbol: "wei"},
		Accounts:     []*config.Account{{Address: "0x0000000000000000000000000000000000000002", Name: "acct"}},
	}

	errs := validator.ValidateNode(node)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid contract address")
	}
}

func TestERC20Validator_NonChecksumContract(t *testing.T) {
	validator := NewERC20Validator()
	node := &config.Node{
		Name:         "erc20-node",
		Module:       "erc20",
		HttpAddr:     "http://localhost:8545",
		ContractAddr: "0x000000000000000000000000000000000000000a",
		Unit:         &currency.Unit{Name: "wei", Symbol: "wei"},
		MetricsUnit:  &currency.Unit{Name: "wei", Symbol: "wei"},
		Accounts:     []*config.Account{{Address: "0x0000000000000000000000000000000000000002", Name: "acct"}},
	}

	errs := validator.ValidateNode(node)
	if len(errs) == 0 {
		t.Fatal("expected checksum validation error")
	}
}

func TestERC20Validator_AutoDiscoveryWithoutUnits(t *testing.T) {
	validator := NewERC20Validator()
	node := &config.Node{
		Name:              "erc20-node",
		Module:            "erc20",
		HttpAddr:          "http://localhost:8545",
		ContractAddr:      "0x0000000000000000000000000000000000000001",
		Accounts:          []*config.Account{{Address: "0x0000000000000000000000000000000000000002", Name: "acct"}},
		AutoUnitDiscovery: true,
	}

	errs := validator.ValidateNode(node)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for auto discovery mode, got %v", errs)
	}
}

func TestERC20Validator_CustomUnitsWithoutDiscovery(t *testing.T) {
	validator := NewERC20Validator()
	node := &config.Node{
		Name:         "erc20-node",
		Module:       "erc20",
		HttpAddr:     "http://localhost:8545",
		ContractAddr: "0x0000000000000000000000000000000000000001",
		Unit: &currency.Unit{
			Name:   "zama",
			Symbol: "ZAMA",
		},
		MetricsUnit: &currency.Unit{
			Name:   "zama",
			Symbol: "ZAMA",
		},
		Accounts: []*config.Account{{Address: "0x0000000000000000000000000000000000000002", Name: "acct"}},
	}

	errs := validator.ValidateNode(node)
	if len(errs) != 0 {
		t.Fatalf("expected custom units to be accepted, got %v", errs)
	}
}
