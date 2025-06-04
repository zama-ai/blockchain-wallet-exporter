# Blockchain Wallet Exporter

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
  environment: production
  metricsAddr: ":9090"
  logLevel: info
  
  # Optional auto-refund configuration
  autoRefund:
    enabled: true
    schedule: "*/5 * * * *"  # Every 5 minutes
    faucetUrl: "https://faucet.example.com"
    timeout: 30

nodes:
  - name: kms-1
    module: cosmos
    grpcAddr: "grpc://127.0.0.1:9090"
    grpcSSLVerify: false
    # unit represents the base unit from the chain
    unit: ucosm # default unit is ucosm for cosmos
    # metricsUnit represents the unit used for Prometheus metrics export
    metricsUnit: cosm # exported metrics unit (optional, defaults to base unit)
    accounts:
      - address: "wasm..."
        name: wasm-1
        # Auto-refund configuration (optional)
        refundThreshold: 100.0  # Trigger refund when balance < 100 ucosm
        refundTarget: 1000.0    # Refund to reach 1000 ucosm
    labels:
      app: kms-blockchain
      env: test
    authorization:
      username: "kms"
      password: "kms"
  - name: fhevm
    module: evm
    httpAddr: "http://127.0.0.1:8545"
    httpSSLVerify: false
    # unit represents the base unit from the chain
    unit: wei # default unit is wei for ethereum
    metricsUnit: eth # exported metrics unit for prometheus
    accounts:
      - address: "0x02933E8678FE8F5D4CFFD0E331A264CD86AEBD8A"
        name: eth-2
        # Auto-refund configuration (optional)
        refundThreshold: 0.1    # Trigger refund when balance < 0.1 eth
        refundTarget: 1.0       # Refund to reach 1.0 eth
    labels:
      app: fhevm
      env: test
```

## Building and Running

### Prerequisites

- Go 1.22.3 or later
- Docker (for building Docker images)
- Prometheus (for metrics collection)

### Build

To build the application, run:

```bash
make build
```

### Run

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


## Contributing

[Specify contribution guidelines here]

## Contact

For any inquiries or issues, please contact ghislain.cheng@zama.ai