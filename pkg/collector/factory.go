package collector

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
)

func NewCollector(node config.Node, currencyRegistry *currency.Registry) (prometheus.Collector, error) {
	var (
		prometheusCollector prometheus.Collector
		err                 error
	)
	switch node.Module {
	case "evm":
		logger.Infof("initializing evm collector for %s", node.Name)
		prometheusCollector, err = NewEVMCollector(&node, currencyRegistry, WithEVMLabels(node.Labels))
		if err != nil {
			return nil, fmt.Errorf("failed to init evm collector: %v", err)
		}
	case "cosmos":
		logger.Infof("initializing cosmos collector for %s", node.Name)
		prometheusCollector, err = NewCosmosCollector(&node, currencyRegistry, WithCosmosLabels(node.Labels))
		if err != nil {
			return nil, fmt.Errorf("failed to init cosmos collector: %v", err)
		}
	case "erc20":
		logger.Infof("initializing erc20 collector for %s", node.Name)
		prometheusCollector, err = NewERC20Collector(&node, currencyRegistry)
		if err != nil {
			return nil, fmt.Errorf("failed to init erc20 collector: %v", err)
		}
	default:
		return nil, fmt.Errorf("invalid module: %s", node.Module)
	}
	return prometheusCollector, nil
}
