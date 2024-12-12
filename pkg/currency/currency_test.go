package currency

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	assert.NotNil(t, r)
	assert.Empty(t, r.units)
}

func TestRegister(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name    string
		unit    *Unit
		wantErr bool
	}{
		{
			name:    "valid registration",
			unit:    DefaultETH,
			wantErr: false,
		},
		{
			name: "empty name",
			unit: &Unit{
				Name:     "",
				Symbol:   "TEST",
				Decimals: 18,
			},
			wantErr: true,
		},
		{
			name:    "duplicate registration",
			unit:    DefaultETH,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Register(tt.unit)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGet(t *testing.T) {
	r := NewDefaultRegistry()

	tests := []struct {
		name    string
		unit    string
		wantErr bool
	}{
		{
			name:    "existing unit",
			unit:    "eth",
			wantErr: false,
		},
		{
			name:    "case insensitive",
			unit:    "ETH",
			wantErr: false,
		},
		{
			name:    "non-existent unit",
			unit:    "NOTFOUND",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit, err := r.Get(tt.unit)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, strings.ToLower(tt.unit), unit.Name)
			}
		})
	}
}

func TestConvert(t *testing.T) {
	r := NewDefaultRegistry()

	tests := []struct {
		name    string
		amount  float64
		from    string
		to      string
		want    float64
		wantErr bool
	}{
		{
			name:    "eth to wei",
			amount:  1.0,
			from:    "eth",
			to:      "wei",
			want:    1e18,
			wantErr: false,
		},
		{
			name:    "wei to eth",
			amount:  1e18,
			from:    "wei",
			to:      "eth",
			want:    1.0,
			wantErr: false,
		},
		{
			name:    "gwei to wei",
			amount:  1.0,
			from:    "gwei",
			to:      "wei",
			want:    1e9,
			wantErr: false,
		},
		{
			name:    "same unit",
			amount:  1.0,
			from:    "eth",
			to:      "eth",
			want:    1.0,
			wantErr: false,
		},
		{
			name:    "invalid from unit",
			amount:  1.0,
			from:    "INVALID",
			to:      "eth",
			want:    0,
			wantErr: true,
		},
		{
			name:    "ucosm to cosm",
			amount:  1.0,
			from:    "ucosm",
			to:      "cosm",
			want:    1e-6,
			wantErr: false,
		},
		{
			name:    "cosm to ucosm",
			amount:  1.0,
			from:    "cosm",
			to:      "ucosm",
			want:    1e6,
			wantErr: false,
		},
		{
			name:    "no conversion rate",
			amount:  1.0,
			from:    "eth",
			to:      "cosm",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Convert(tt.amount, tt.from, tt.to)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    *Unit
		wantErr bool
	}{
		{
			name:    "valid ETH",
			yaml:    "ETH",
			want:    DefaultETH,
			wantErr: false,
		},
		{
			name:    "invalid unit",
			yaml:    "INVALID",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var unit Unit
			err := yaml.Unmarshal([]byte(tt.yaml), &unit)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want.Name, unit.Name)
				assert.Equal(t, tt.want.Symbol, unit.Symbol)
				assert.Equal(t, tt.want.Decimals, unit.Decimals)
			}
		})
	}
}

func TestList(t *testing.T) {
	r := NewDefaultRegistry()
	units := r.List()

	// Check if all default units are present
	assert.Len(t, units, 5) // ETH, WEI, GWEI, COSM, UCOSM

	// Verify presence of specific units
	names := make(map[string]bool)
	for _, unit := range units {
		names[unit.Name] = true
	}

	assert.True(t, names["eth"])
	assert.True(t, names["wei"])
	assert.True(t, names["gwei"])
	assert.True(t, names["cosm"])
	assert.True(t, names["ucosm"])
}

func TestMustRegisterAndMustGet(t *testing.T) {
	r := NewRegistry()

	// Test MustRegister success
	assert.NotPanics(t, func() {
		r.MustRegister(DefaultETH)
	})

	// Test MustRegister panic
	assert.Panics(t, func() {
		r.MustRegister(DefaultETH) // Duplicate registration
	})

	// Test MustGet success
	assert.NotPanics(t, func() {
		unit := r.MustGet("ETH")
		assert.Equal(t, "eth", unit.Name)
	})

	// Test MustGet panic
	assert.Panics(t, func() {
		r.MustGet("NOTFOUND")
	})
}
