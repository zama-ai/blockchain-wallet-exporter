package validation

import (
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
)

// BaseValidator provides common validation logic for all node types
type BaseValidator struct{}

func (v *BaseValidator) ValidateCommonFields(node *config.Node) ValidationErrors {
	var errors ValidationErrors

	// Validate node name
	if node.Name == "" {
		errors = append(errors, ValidationError{
			Field:   "name",
			Message: "node name cannot be empty",
		})
	}

	// Validate accounts
	if len(node.Accounts) == 0 {
		errors = append(errors, ValidationError{
			Field:   "accounts",
			Message: "at least one account must be specified",
		})
	}

	return errors
}
