# Blockchain Wallet Exporter

[![CI](https://github.com/zama-ai/blockchain-wallet-exporter/actions/workflows/ci.yaml/badge.svg)](https://github.com/zama-ai/blockchain-wallet-exporter/actions/workflows/ci.yaml)
[![codecov](https://codecov.io/gh/zama-ai/blockchain-wallet-exporter/branch/main/graph/badge.svg)](https://codecov.io/gh/zama-ai/blockchain-wallet-exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/zama-ai/blockchain-wallet-exporter)](https://goreportcard.com/report/github.com/zama-ai/blockchain-wallet-exporter)

## Overview

The Blockchain Wallet Exporter is a tool designed to retrieve and export wallet balances from different blockchains, specifically Cosmos and Ethereum, as metrics. It is built using Golang for seamless integration with Prometheus and includes an optional auto-refund feature for maintaining wallet balances.

## Features

- **Minimalistic Design**: Focused on essential functionalities to keep the tool lightweight.
- **Multi-Blockchain Support**: Currently supports Cosmos and Ethereum blockchains.
- **Concurrent Balance Retrieval**: Utilizes a worker pool to fetch balances for multiple wallets concurrently.
- **Flexible Unit Selection**: Allows balance retrieval in different units (e.g., wei, gwei, eth for Ethereum, and ucosm, cosm for Cosmos).
- **Prometheus Integration**: Exports metrics in a format compatible with Prometheus.
- **Manual Wallet Configuration**: Wallets need to be manually configured as auto-discovery is not supported.
- **Auto-Refund System**: Optional automated wallet funding when balances fall below configurable thresholds using faucet APIs.

## Goals

- Retrieve wallet balances for Cosmos and Ethereum.
- Export balances as metrics.
- Support concurrent balance retrieval.
- Provide consistent label keys across all wallets.
- Default balance unit is wei for Ethereum and ucosm for Cosmos.
- Automatically maintain wallet balances above specified thresholds.

## Non-Goals

- No support for multi-instance or cluster setups.
- Does not pull metrics from validators.
- No SSL metrics endpoint support currently.

## Configuration

The exporter is configured using a YAML file. Below is an example configuration:

```yaml
global:
  environment: development
  metricsAddr: ":9091"
  logLevel: debug
 
  # Gateway node with explicit autoRefund configuration
nodes:
  - name: l2-gateway
    module: evm
    httpAddr: "https://arbitrum-one-rpc.publicnode.com"
    
    # Explicit auto-refund configuration for this node
    autoRefund:
      enabled: false
      schedule: "@every 1m"  # Frequent checks for gateway
      faucetUrl: "http://localhost:8080"  # Gateway-specific faucet
      timeout: 30
    
    unit: wei          # Native blockchain unit
    metricsUnit: eth   # Human-readable unit for metrics and comparisons
    httpSSLVerify: false
    accounts:
      - address: "0x5Fadfae7fDCa560e377FF26F26760891251De983"
        name: test-1-l2
        # Auto-refund thresholds for this account
        refundThreshold: 0.8  # Trigger refund when balance below refundThreshold to reach refundTarget 
        refundTarget: 1.2     

      - address: "0xa0d2763f12D09efacfdEf6d5656d5Be7bB3E9600"
        name: test-2-l2
        # Different thresholds for backup account
        refundThreshold: 1.3  # Lower threshold for backup
        refundTarget: 1.6     # Lower target for backup

    labels:
      app: l2-gateway
      env: test

  # example with erc20 token
  - name: conduit-gateway-erc20
    module: erc20
    httpAddr: "https://arbitrum-one-rpc.publicnode.com"
    # Automatically discover ERC20 token metadata (symbol, decimals) during startup
    # If RPC endpoint is unreachable, the exporter will fail to start
    autoUnitDiscovery: true
    # Explicit auto-refund configuration for this node
    autoRefund:
      enabled: false
      schedule: "@every 1m"  # Frequent checks for gateway
      faucetUrl: "http://localhost:8080"  # Gateway-specific faucet
      timeout: 30
    
    httpSSLVerify: false
    contractAddress: "0xB38031951903f798f7114d726c17aB04574e82e8"
    accounts:
      - address: "0xe6DE4F4E3A17be1D0c9d69a0574e78678CDd5e29"
        name: account-erc20

    labels:
      app: l3-rollup
      env: test


  # Host node with explicit autoRefund configuration
  - name: l1-host
    module: evm
    # public node RPC sepolia
    httpAddr: "https://ethereum-sepolia-rpc.publicnode.com"
    
    # Explicit auto-refund configuration for this node
    autoRefund:
      enabled: false
      schedule: "@every 1m"  # Frequent checks for host
      faucetUrl: "http://localhost:8081"  # Gateway-specific faucet
      timeout: 30
    
    unit: wei          # Native blockchain unit
    metricsUnit: eth   # Human-readable unit for metrics and comparisons
    httpSSLVerify: false
    accounts:
      - address: "0xe6DE4F4E3A17be1D0c9d69a0574e78678CDd5e29"
        name: test-1-l1
        # Auto-refund thresholds for this account
        refundThreshold: 1.3  # Trigger refund when balance below refundThreshold to reach refundTarget
        refundTarget: 1.6     

      - address: "0xfEB8B994c932B00796c52C0df0cbF09eAA010890"
        name: test-2-l1
        # Different thresholds for backup account
        refundThreshold: 1.7  # Lower threshold for backup
        refundTarget: 2.0     # Lower target for backup

    labels:
      app: l1-host
      env: test

```

## Installation

### Kubernetes with Helm (Recommended)

The easiest way to deploy the Blockchain Wallet Exporter on Kubernetes is using our Helm chart:

```bash
# Install from GitHub Container Registry
helm install blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.1 \
  --namespace monitoring \
  --create-namespace

# Or install with custom configuration
helm install blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.1 \
  --values values.yaml
```

For detailed Helm chart documentation, see the [chart README](charts/blockchain-wallet-exporter/README.md).

### Building and Running Locally

#### Prerequisites

- Go 1.23.x
- Docker (for building Docker images)
- Prometheus (for metrics collection)

#### Build

To build the application, run:

```bash
make build
```

#### Run

To run the application using Docker Compose, execute:

```bash
make run
```

### Testing

To run tests and generate a coverage report, use:

```bash
make test
```

### Linting

To run the linter, execute:

```bash
make test-lint
```

## License

Licensed under the **Apache License 2.0**. See [`LICENSE`](LICENSE).

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for development workflow, testing/linting, and PR guidelines.

## Contact

For any inquiries or issues, please contact ghislain.cheng@zama.ai