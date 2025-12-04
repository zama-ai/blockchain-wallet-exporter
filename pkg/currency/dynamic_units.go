package currency

import (
	"fmt"
	"math"
	"strings"
)

// EnsureUnitPair registers or updates conversion rules between an atomic base unit
// and a display unit for ERC20 tokens.
func EnsureUnitPair(registry *Registry, baseUnitName, displayUnitName string, decimals uint8) error {
	if registry == nil {
		return fmt.Errorf("currency registry cannot be nil")
	}

	baseUnitName = strings.ToLower(strings.TrimSpace(baseUnitName))
	displayUnitName = strings.ToLower(strings.TrimSpace(displayUnitName))

	if baseUnitName == "" {
		return fmt.Errorf("base unit name cannot be empty")
	}

	baseUnit, err := ensureUnit(registry, baseUnitName, 0)
	if err != nil {
		return fmt.Errorf("failed to register base unit %s: %w", baseUnitName, err)
	}

	if displayUnitName == "" || displayUnitName == baseUnitName {
		// Nothing else to register; conversions remain identity
		return nil
	}

	displayUnit, err := ensureUnit(registry, displayUnitName, int(decimals))
	if err != nil {
		return fmt.Errorf("failed to register display unit %s: %w", displayUnitName, err)
	}

	factor := math.Pow10(int(decimals))

	if baseUnit.ConversionTo == nil {
		baseUnit.ConversionTo = map[string]float64{}
	}
	if displayUnit.ConversionTo == nil {
		displayUnit.ConversionTo = map[string]float64{}
	}

	baseUnit.ConversionTo[displayUnitName] = 1 / factor
	displayUnit.ConversionTo[baseUnitName] = factor

	return nil
}

func ensureUnit(registry *Registry, name string, decimals int) (*Unit, error) {
	unit, err := registry.Get(name)
	if err == nil {
		return unit, nil
	}

	unit = &Unit{
		Name:         name,
		Symbol:       name,
		Decimals:     decimals,
		ChainType:    "erc20",
		Description:  fmt.Sprintf("%s unit", name),
		ConversionTo: map[string]float64{},
	}

	_, err = registry.Register(unit)
	if err != nil {
		return nil, err
	}

	return unit, nil
}
