# Blockchain Wallet Exporter

[![CI](https://github.com/zama-ai/blockchain-wallet-exporter/actions/workflows/ci.yaml/badge.svg)](https://github.com/zama-ai/blockchain-wallet-exporter/actions/workflows/ci.yaml)
[![codecov](https://codecov.io/gh/zama-ai/blockchain-wallet-exporter/branch/main/graph/badge.svg)](https://codecov.io/gh/zama-ai/blockchain-wallet-exporter)
[![Go Report Card](https://goreportcard.com/badge/github.com/zama-ai/blockchain-wallet-exporter)](https://goreportcard.com/report/github.com/zama-ai/blockchain-wallet-exporter)

## Overview

The Blockchain Wallet Exporter is a tool designed to retrieve and export wallet balances from different blockchains, specifically Cosmos, Ethereum, and ERC20 tokens, as metrics. It is built using Golang for seamless integration with Prometheus and includes an optional auto-refund feature for maintaining wallet balances.

## Features

- **Minimalistic Design**: Focused on essential functionalities to keep the tool lightweight.
- **Multi-Blockchain Support**: Currently supports Cosmos, Ethereum, and ERC20 tokens.
- **ERC20 Token Support**: Full support for monitoring ERC20 token balances with automatic metadata discovery (symbol, decimals, name).
- **Concurrent Balance Retrieval**: Utilizes a worker pool to fetch balances for multiple wallets concurrently.
- **Flexible Unit Selection**: Allows balance retrieval in different units (e.g., wei, gwei, eth for Ethereum; ucosm, cosm for Cosmos; and automatic unit discovery for ERC20 tokens).
- **Prometheus Integration**: Exports metrics in a format compatible with Prometheus.
- **Manual Wallet Configuration**: Wallets need to be manually configured as auto-discovery is not supported.
- **Auto-Refund System**: Optional automated wallet funding when balances fall below configurable thresholds (works with native tokens and ERC20 tokens). **Note: Requires [POWFaucet](https://github.com/pk910/PoWFaucet) as the faucet backend.**

## Goals

- Retrieve wallet balances for Cosmos, Ethereum, and ERC20 tokens.
- Export balances as metrics.
- Support concurrent balance retrieval.
- Provide consistent label keys across all wallets.
- Default balance unit is wei for Ethereum, ucosm for Cosmos, and atomic units for ERC20 tokens.
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
    # or use environment variable
    # httpAddrEnv: URL_WITH_API_KEY


    # Explicit auto-refund configuration for this node
    # Requires POWFaucet: https://github.com/pk910/PoWFaucet
    autoRefund:
      enabled: false
      schedule: "@every 1m"  # Frequent checks for gateway
      faucetUrl: "http://localhost:8080"  # POWFaucet API endpoint
      timeout: 30
    
    unit: wei          # Native blockchain unit
    metricsUnit: eth   # Human-readable unit for metrics and comparisons
    httpSSLVerify: false
    accounts:
      - address: "0x5Fadfae7fDCa560e377FF26F26760891251De983"
        # or use environment variable
        # addressEnv: WALLET_ADDRESS
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
    # Requires POWFaucet: https://github.com/pk910/PoWFaucet
    autoRefund:
      enabled: false
      schedule: "@every 1m"  # Frequent checks for gateway
      faucetUrl: "http://localhost:8080"  # POWFaucet API endpoint
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
    # Requires POWFaucet: https://github.com/pk910/PoWFaucet
    autoRefund:
      enabled: false
      schedule: "@every 1m"  # Frequent checks for host
      faucetUrl: "http://localhost:8081"  # POWFaucet API endpoint
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

### ERC20 Token Configuration

The exporter provides comprehensive support for monitoring ERC20 token balances with the following capabilities:

#### Key Configuration Options

- **module**: Set to `erc20` to enable ERC20 token monitoring
- **contractAddress**: The ERC20 token contract address (required, must be in checksum format)
- **autoUnitDiscovery**: When enabled, automatically fetches token metadata (symbol, decimals, name) from the contract at startup
  - If `true`, the exporter will discover units dynamically from the contract
  - If `false`, you must manually specify `unit` and `metricsUnit`
  - If the RPC endpoint is unreachable during startup with auto-discovery enabled, the exporter will fail to start

#### Automatic Unit Discovery

When `autoUnitDiscovery: true` is set, the exporter will:
1. Connect to the ERC20 contract via the specified RPC endpoint
2. Call the standard ERC20 methods: `symbol()`, `decimals()`, and `name()`
3. Automatically register currency units based on the token's decimals
4. Use the token symbol for the metrics unit (e.g., `usdt` for USDT)
5. Create an atomic unit name (e.g., `usdt_atomic` for the smallest token unit)

#### Manual Unit Configuration

If you prefer to manually configure units or the contract doesn't support standard ERC20 metadata methods:

```yaml
nodes:
  - name: custom-token
    module: erc20
    httpAddr: "https://your-rpc-endpoint.com"
    autoUnitDiscovery: false  # Disable auto-discovery
    unit: custom_atomic       # Manually specify base unit
    metricsUnit: custom       # Manually specify metrics unit
    contractAddress: "0xYourTokenContractAddress"
    accounts:
      - address: "0xYourWalletAddress"
        name: wallet-1
```

#### ERC20 with Auto-Refund

ERC20 tokens fully support the auto-refund feature, allowing you to automatically maintain token balances. **The auto-refund feature requires [POWFaucet](https://github.com/pk910/PoWFaucet) as the backend faucet service.**

```yaml
nodes:
  - name: usdt-monitor
    module: erc20
    httpAddr: "https://mainnet.infura.io/v3/YOUR-PROJECT-ID"
    autoUnitDiscovery: true
    contractAddress: "0xdac17f958d2ee523a2206206994597c13d831ec7"  # USDT contract
    autoRefund:
      enabled: true
      schedule: "@every 5m"
      faucetUrl: "https://your-powfaucet-instance.com"  # POWFaucet API endpoint
      timeout: 30
    accounts:
      - address: "0xYourWalletAddress"
        name: trading-bot
        refundThreshold: 100    # Trigger refund when below 100 USDT
        refundTarget: 500        # Refund to 500 USDT
```

#### Important Notes

- The contract address must be a valid hex-encoded address in checksum format
- The exporter validates ERC20 addresses using Ethereum's checksum validation
- ERC20 balance queries use the standard `balanceOf(address)` method
- Currency conversion from atomic units to human-readable units is handled automatically
- Per the ERC20 specification, `symbol()` and `decimals()` are required, but `name()` is optional (falls back to symbol if unavailable)
- **Auto-Refund Requirement**: The auto-refund feature exclusively works with [POWFaucet](https://github.com/pk910/PoWFaucet). The exporter uses POWFaucet's API endpoints (`/api/startSession`, `/api/claimReward`, `/api/getSessionStatus`) for funding operations. Other faucet implementations are not supported.

## Metrics and Monitoring

The exporter provides comprehensive Prometheus metrics for monitoring wallet balances and health:

### Exported Metrics

- **`blockchain_wallet_balance`**: Current balance of wallet accounts (Gauge)
- **`blockchain_wallet_health`**: Health status of wallet accounts (Gauge, 1=healthy, 0=unhealthy)
- **`blockchain_wallet_scrapes_failed_total`**: Total number of failed scrape cycles (Counter)

All metrics include rich labels for filtering and aggregation:
- `address`, `account_name`: Account identifiers
- `node_name`, `module`, `unit`: Configuration identifiers
- `exporter_version`: Exporter version
- Custom labels from configuration

### Quick Start

Access metrics at: `http://localhost:9091/metrics`

Example Prometheus scrape configuration:

```yaml
scrape_configs:
  - job_name: 'blockchain-wallet-exporter'
    scrape_interval: 30s
    static_configs:
      - targets: ['localhost:9091']
```

### Monitoring Examples

**Check account balance:**
```promql
blockchain_wallet_balance{account_name="test-1-l2"}
```

**Alert on low balance:**
```promql
blockchain_wallet_balance < 0.5
```

**Monitor account health:**
```promql
blockchain_wallet_health == 0
```

### Complete Documentation

For comprehensive metrics documentation including:
- Detailed metric descriptions and label reference
- PromQL query patterns and examples
- Recording and alerting rules
- Grafana dashboard examples
- Troubleshooting guide

See **[Metrics Documentation](docs/Metrics.md)**.

## Installation

### Kubernetes with Helm (Recommended)

The easiest way to deploy the Blockchain Wallet Exporter on Kubernetes is using our Helm chart:

```bash
# Install from GitHub Container Registry
helm install blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.2 \
  --namespace monitoring \
  --create-namespace

# Or install with custom configuration
helm install blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.2 \
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

## Documentation

- **[Metrics Documentation](docs/Metrics.md)**: Complete guide to Prometheus metrics, queries, alerting, and monitoring
- **[Configuration Specification](docs/Spec.md)**: Detailed configuration options and examples
- **[Helm Chart README](charts/blockchain-wallet-exporter/README.md)**: Kubernetes deployment guide

## License

Licensed under the **Apache License 2.0**. See [`LICENSE`](LICENSE).

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for development workflow, testing/linting, and PR guidelines.

## Contact

For any inquiries or issues, please contact ghislain.cheng@zama.ai