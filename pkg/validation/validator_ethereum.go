package validation

import (
	"fmt"
	"net/url"

	"github.com/ethereum/go-ethereum/common"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
)

type EthereumValidator struct {
	BaseValidator
}

func NewEthereumValidator() *EthereumValidator {
	return &EthereumValidator{}
}

func (v *EthereumValidator) ValidateNode(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	// Validate common fields
	errors = append(errors, v.ValidateCommonFields(node)...)

	// Validate HTTP address
	if err := v.validateHTTPAddress(node); err != nil {
		errors = append(errors, err...)
	}

	// Validate unit
	if err := v.validateUnit(node); err != nil {
		errors = append(errors, err...)
	}

	// Validate accounts
	if err := v.validateAccounts(node); err != nil {
		errors = append(errors, err...)
	}

	return errors
}

func (v *EthereumValidator) validateHTTPAddress(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	if node.HttpAddr == "" {
		errors = append(errors, ValidationError{
			Field:   "httpAddr",
			Message: "HTTP address cannot be empty",
		})
		return errors
	}

	parsedURL, err := url.Parse(node.HttpAddr)
	if err != nil {
		errors = append(errors, ValidationError{
			Field:   "httpAddr",
			Message: "invalid HTTP address URL",
		})
		return errors
	}

	// Validate scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		errors = append(errors, ValidationError{
			Field:   "httpAddr",
			Message: "URL scheme must be either http or https",
		})
	}

	// Validate SSL settings for HTTPS
	if parsedURL.Scheme == "https" {
		if node.HttpSSLVerify == "" {
			errors = append(errors, ValidationError{
				Field:   "httpSSLVerify",
				Message: "SSL verification setting must be specified for HTTPS connections",
			})
		} else if node.HttpSSLVerify != "true" && node.HttpSSLVerify != "false" {
			errors = append(errors, ValidationError{
				Field:   "httpSSLVerify",
				Message: "SSL verification must be either 'true' or 'false'",
			})
		}
	}

	return errors
}

func (v *EthereumValidator) validateUnit(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	if node.Unit == nil {
		errors = append(errors, ValidationError{
			Field:   "unit",
			Message: "unit cannot be empty",
		})
		return errors
	}

	// Validate that the unit is either ETH or WEI
	//unit := node.Unit.Name
	//if unit != currency.DefaultETH.Name && unit != currency.DefaultWEI.Name && unit != currency.DefaultGWEI.Name {
	//	errors = append(errors, ValidationError{
	//		Field:   "unit",
	//		Message: "unit must be either ETH, WEI or GWEI for Ethereum chains",
	//	})
	//}

	return v.validateUnitWithConfig(node, UnitValidationConfig{
		ValidUnits: []string{currency.DefaultETH.Name, currency.DefaultWEI.Name, currency.DefaultGWEI.Name},
		UnitType:   currency.DefaultETH.ChainType,
	})
}

func (v *EthereumValidator) validateAccounts(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	for _, account := range node.Accounts {
		if account.Address == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("account %s", account.Name),
				Message: "account address cannot be empty",
			})
			continue
		}

		if !common.IsHexAddress(account.Address) {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("account %s", account.Name),
				Message: "invalid Ethereum address format",
			})
		}

		// Normalize address to checksum format
		checksumAddr := common.HexToAddress(account.Address).Hex()
		if account.Address != checksumAddr {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("account %s", account.Name),
				Message: fmt.Sprintf("address should be in checksum format: %s", checksumAddr),
			})
		}
	}

	return errors
}
