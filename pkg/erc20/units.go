package erc20

import (
	"context"
	"fmt"
	"strings"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
)

// ResolveUnits fetches metadata for the ERC20 node, registers units, and applies config fallbacks.
func ResolveUnits(ctx context.Context, registry *currency.Registry, node *config.Node) (*Metadata, error) {
	if node == nil {
		return nil, fmt.Errorf("node configuration cannot be nil")
	}

	if !node.IsERC20Module() {
		return nil, nil
	}

	client, err := NewClient(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("failed to init erc20 client for node %s: %w", node.Name, err)
	}
	defer client.Close()

	meta, err := client.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch token metadata for node %s: %w", node.Name, err)
	}

	baseUnitName := node.BaseUnitName()
	metricsUnitName := node.MetricsUnitName()

	if node.AutoUnitDiscovery {
		if baseUnitName == "" {
			baseUnitName = fmt.Sprintf("%s_atomic", strings.ToLower(meta.Symbol))
		}
		if metricsUnitName == "" {
			metricsUnitName = strings.ToLower(meta.Symbol)
		}
	}

	if baseUnitName == "" || metricsUnitName == "" {
		return nil, fmt.Errorf("node %s missing unit definitions and auto discovery disabled", node.Name)
	}

	if err := currency.EnsureUnitPair(registry, baseUnitName, metricsUnitName, meta.Decimals); err != nil {
		return nil, fmt.Errorf("failed to register units for node %s: %w", node.Name, err)
	}

	baseUnit, err := registry.Get(baseUnitName)
	if err != nil {
		return nil, fmt.Errorf("base unit %s not registered: %w", baseUnitName, err)
	}
	metricsUnit, err := registry.Get(metricsUnitName)
	if err != nil {
		return nil, fmt.Errorf("metrics unit %s not registered: %w", metricsUnitName, err)
	}

	node.Unit = baseUnit
	node.UnitSet = true
	node.MetricsUnit = metricsUnit
	node.MetricsUnitSet = true
	node.ResolvedUnit = baseUnitName
	node.ResolvedMetrics = metricsUnitName

	return meta, nil
}
