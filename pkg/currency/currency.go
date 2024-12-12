package currency

import (
	"fmt"
	"strings"
)

// Unit represents a currency unit
type Unit struct {
	Name         string
	Symbol       string
	Decimals     int
	ChainType    string // e.g., "evm", "cosmos"
	Description  string
	ConversionTo map[string]float64 // Conversion rates to other units
}

// Registry maintains a global registry of currency units
type Registry struct {
	units map[string]*Unit
}

var (
	// Pre-defined currency units configurations (but not registered)
	DefaultETH = &Unit{
		Name:         "eth",
		Symbol:       "eth",
		Decimals:     18,
		ChainType:    "evm",
		Description:  "Ethereum",
		ConversionTo: map[string]float64{"wei": 1e18, "gwei": 1e9},
	}

	DefaultGWEI = &Unit{
		Name:         "gwei",
		Symbol:       "gwei",
		Decimals:     9,
		ChainType:    "evm",
		Description:  "Gwei",
		ConversionTo: map[string]float64{"eth": 1e-9, "wei": 1e9},
	}

	DefaultWEI = &Unit{
		Name:         "wei",
		Symbol:       "wei",
		Decimals:     0,
		ChainType:    "evm",
		Description:  "Wei (smallest Ethereum unit)",
		ConversionTo: map[string]float64{"eth": 1e-18, "gwei": 1e-9},
	}

	DefaultCOSM = &Unit{
		Name:         "cosm",
		Symbol:       "cosm",
		Decimals:     6,
		ChainType:    "cosmos",
		Description:  "Cosmos USD",
		ConversionTo: map[string]float64{"ucosm": 1e6},
	}

	DefaultUCOSM = &Unit{
		Name:         "ucosm",
		Symbol:       "ucosm",
		Decimals:     0,
		ChainType:    "cosmos",
		Description:  "Cosmos smallest unit",
		ConversionTo: map[string]float64{"cosm": 1e-6},
	}
)

// NewRegistry creates a new currency registry
func NewRegistry() *Registry {
	return &Registry{
		units: make(map[string]*Unit),
	}
}

// Register adds a new currency unit to the registry
func (r *Registry) Register(unit *Unit) (*Unit, error) {
	if unit.Name == "" {
		return nil, fmt.Errorf("currency unit name cannot be empty")
	}

	normalizedName := strings.ToLower(unit.Name)
	if _, exists := r.units[normalizedName]; exists {
		return nil, fmt.Errorf("currency unit %s already registered", normalizedName)
	}

	r.units[normalizedName] = unit
	return unit, nil
}

// MustRegister is like Register but panics on error
func (r *Registry) MustRegister(unit *Unit) *Unit {
	u, err := r.Register(unit)
	if err != nil {
		panic(err)
	}
	return u
}

// Get retrieves a currency unit from the registry
func (r *Registry) Get(name string) (*Unit, error) {
	normalizedName := strings.ToLower(name)
	unit, exists := r.units[normalizedName]
	if !exists {
		return nil, fmt.Errorf("currency unit %s not found", name)
	}
	return unit, nil
}

// MustGet is like Get but panics on error
func (r *Registry) MustGet(name string) *Unit {
	unit, err := r.Get(name)
	if err != nil {
		panic(err)
	}
	return unit
}

// List returns all registered currency units
func (r *Registry) List() []*Unit {
	units := make([]*Unit, 0, len(r.units))
	for _, unit := range r.units {
		units = append(units, unit)
	}
	return units
}

// Convert converts an amount from one currency unit to another
func (r *Registry) Convert(amount float64, from, to string) (float64, error) {
	fromUnit, err := r.Get(from)
	if err != nil {
		return 0, err
	}

	// If converting to same unit, return original amount
	if from == to {
		return amount, nil
	}

	if rate, exists := fromUnit.ConversionTo[to]; exists {
		return amount * rate, nil
	}

	return 0, fmt.Errorf("no conversion rate found from %s to %s", from, to)
}

// UnmarshalYAML implements yaml.Unmarshaler
func (u *Unit) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}

	// Create a default registry for unmarshaling
	registry := NewDefaultRegistry()
	unit, err := registry.Get(name)
	if err != nil {
		return err
	}

	*u = *unit
	return nil
}

// String returns the string representation of the currency unit
func (u *Unit) String() string {
	return u.Symbol
}

// Helper to create a new registry with default units
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.MustRegister(DefaultETH)
	r.MustRegister(DefaultWEI)
	r.MustRegister(DefaultGWEI)
	r.MustRegister(DefaultCOSM)
	r.MustRegister(DefaultUCOSM)
	return r
}
