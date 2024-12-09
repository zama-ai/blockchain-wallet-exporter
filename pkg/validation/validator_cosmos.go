package validation

import (
	"fmt"
	"net/url"

	"github.com/cosmos/btcutil/bech32"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
)

type CosmosValidator struct {
	BaseValidator
}

func NewCosmosValidator() *CosmosValidator {
	return &CosmosValidator{}
}

func (v *CosmosValidator) ValidateNode(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	// Validate common fields
	errors = append(errors, v.ValidateCommonFields(node)...)

	// Validate GRPC address
	if err := v.validateGRPCAddress(node); err != nil {
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

func (v *CosmosValidator) validateGRPCAddress(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	if node.GrpcAddr == "" {
		errors = append(errors, ValidationError{
			Field:   "grpcAddr",
			Message: "GRPC address cannot be empty",
		})
		return errors
	}

	parsedURL, err := url.Parse(node.GrpcAddr)
	if err != nil {
		errors = append(errors, ValidationError{
			Field:   "grpcAddr",
			Message: "invalid GRPC address URL",
		})
		return errors
	}

	// Validate port is present
	if parsedURL.Port() == "" {
		errors = append(errors, ValidationError{
			Field:   "grpcAddr",
			Message: "GRPC address must include a port number",
		})
	}

	return errors
}

func (v *CosmosValidator) validateUnit(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	if node.Unit == nil {
		errors = append(errors, ValidationError{
			Field:   "unit",
			Message: "unit cannot be empty",
		})
		return errors
	}

	if node.Unit.Name != currency.DefaultCOSM.Name && node.Unit.Name != currency.DefaultUCOSM.Name {
		errors = append(errors, ValidationError{
			Field:   "unit",
			Message: "unit must be COSM or UCOSM for Cosmos chains",
		})
	}

	return errors
}

func (v *CosmosValidator) validateAccounts(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	for i, account := range node.Accounts {
		if account.Address == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("accounts[%d].address", i),
				Message: "account address cannot be empty",
			})
			continue
		}

		// Validate Cosmos address format by checking the HRP
		hrp, _, err := bech32.Decode(account.Address, bech32.MaxLengthBIP173)
		if err != nil {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("accounts[%d].address", i),
				Message: "invalid Cosmos address format",
			})
			continue
		}

		if hrp != "wasm" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("accounts[%d].address", i),
				Message: "invalid Cosmos address format",
			})
		}

	}

	return errors
}
