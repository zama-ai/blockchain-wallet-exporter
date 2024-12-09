package validation

import (
	"fmt"
	"strings"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

var (
	mapValidators = map[string]NodeValidator{
		"cosmos": NewCosmosValidator(),
		"evm":    NewEthereumValidator(),
	}
)

// ValidationError represents a validation error with a specific field and message
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors holds multiple validation errors
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	var errMsgs []string
	for _, err := range e {
		errMsgs = append(errMsgs, err.Error())
	}
	return strings.Join(errMsgs, "; ")
}

// NodeValidator defines the interface for node-specific validators
type NodeValidator interface {
	ValidateNode(node *config.Node) ValidationErrors
}

// ConfigValidator handles validation of the entire configuration
type ConfigValidator struct {
	validators map[string]NodeValidator
}

// NewConfigValidator creates a new ConfigValidator with registered node validators
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{
		validators: mapValidators,
	}
}

// ValidateConfig validates the entire configuration schema
func (v *ConfigValidator) ValidateConfig(cfg *config.Schema) error {
	var allErrors ValidationErrors

	// Validate global configuration
	if errs := v.validateGlobal(&cfg.Global); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// Validate each node configuration
	for _, node := range cfg.Nodes {
		validator, exists := v.validators[node.Module]
		if !exists {
			allErrors = append(allErrors, ValidationError{
				Field:   "module",
				Message: fmt.Sprintf("unsupported module type: %s", node.Module),
			})
			continue
		}

		if errs := validator.ValidateNode(&node); len(errs) > 0 {
			allErrors = append(allErrors, errs...)
		}
	}

	if len(allErrors) > 0 {
		return allErrors
	}
	return nil
}

// validateGlobal validates the global configuration
func (v *ConfigValidator) validateGlobal(global *config.Global) ValidationErrors {
	var errors ValidationErrors
	logger.Infof("validating global config: %v", global)

	if global.MetricsAddr == "" {
		errors = append(errors, ValidationError{
			Field:   "global.metricsAddr",
			Message: "cannot be empty",
		})
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[strings.ToLower(global.LogLevel)] {
		errors = append(errors, ValidationError{
			Field:   "global.logLevel",
			Message: "must be one of: debug, info, warn, error",
		})
	}

	return errors
}
