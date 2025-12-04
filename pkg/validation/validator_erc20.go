package validation

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
)

// ERC20Validator reuses EVM validation with additional ERC20-specific checks
type ERC20Validator struct {
	evm *EthereumValidator
}

func NewERC20Validator() *ERC20Validator {
	return &ERC20Validator{
		evm: NewEthereumValidator(),
	}
}

func (v *ERC20Validator) ValidateNode(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	// Run standard EVM validations first
	errors = append(errors, v.evm.ValidateNode(node)...)

	// ERC20 nodes may opt-in to auto discovery for units
	if !node.AutoUnitDiscovery {
		if node.Unit == nil {
			errors = append(errors, ValidationError{
				Field:   "unit",
				Message: "unit is required when autoUnitDiscovery is disabled",
			})
		}
		if node.MetricsUnit == nil {
			errors = append(errors, ValidationError{
				Field:   "metricsUnit",
				Message: "metricsUnit is required when autoUnitDiscovery is disabled",
			})
		}
	}

	// Ensure contract address present and valid
	if node.ContractAddr == "" {
		errors = append(errors, ValidationError{
			Field:   "contractAddress",
			Message: "contractAddress cannot be empty for erc20 module",
		})
		return errors
	}

	if !common.IsHexAddress(node.ContractAddr) {
		errors = append(errors, ValidationError{
			Field:   "contractAddress",
			Message: "contractAddress must be a valid hex-encoded address",
		})
		return errors
	}

	checksum := common.HexToAddress(node.ContractAddr).Hex()
	if node.ContractAddr != checksum {
		errors = append(errors, ValidationError{
			Field:   "contractAddress",
			Message: fmt.Sprintf("contractAddress should be in checksum format: %s", checksum),
		})
	}

	return errors
}
