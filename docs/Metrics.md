# Metrics Documentation

This document describes all the Prometheus metrics exported by the Blockchain Wallet Exporter.

## Overview

The Blockchain Wallet Exporter exposes metrics on the `/metrics` endpoint (default: `:9091/metrics`). All metrics are prefixed with `blockchain_wallet_` and include comprehensive labels for filtering and aggregation.

## Metrics

### 1. blockchain_wallet_balance

**Type:** Gauge

**Description:** Current balance of blockchain wallet accounts in the configured metrics unit.

**Labels:**
- `address` (variable): The blockchain address of the wallet
- `account_name` (variable): The human-readable name assigned to the account
- `node_name` (constant): The name of the node configuration
- `module` (constant): The blockchain module type (`evm`, `cosmos`, or `erc20`)
- `unit` (constant): The unit of the balance value (e.g., `eth`, `wei`, `cosm`, `ucosm`, or token symbol for ERC20)
- `exporter_version` (constant): The version of the exporter
- Custom labels (constant): Any additional labels defined in the node configuration

**Value:** The wallet balance as a floating-point number in the specified unit.

**Example:**
```promql
# Get balance for a specific account
blockchain_wallet_balance{account_name="test-1-l2", node_name="l2-gateway"}

# Get all balances for a specific module
blockchain_wallet_balance{module="evm"}

# Get balances in eth for production environment
blockchain_wallet_balance{unit="eth", env="production"}

# Sum all balances for a specific application
sum(blockchain_wallet_balance{app="l2-gateway"})

# Alert when balance falls below threshold
blockchain_wallet_balance{account_name="test-1-l2"} < 0.5
```

**Notes:**
- The balance value is returned in the `metricsUnit` if configured, otherwise in the base `unit`
- For EVM: typically shown in `eth` (metricsUnit) converted from `wei` (native unit)
- For Cosmos: typically shown in `cosm` (metricsUnit) converted from `ucosm` (native unit)
- For ERC20: shown in the token's human-readable unit (e.g., `usdt`) converted from atomic units
- If an account's health check fails (health = 0), the balance metric is not exported for that scrape

### 2. blockchain_wallet_health

**Type:** Gauge

**Description:** Health status of blockchain wallet accounts. A value of `1` indicates the account is healthy and balance was successfully retrieved. A value of `0` indicates an error occurred during balance retrieval.

**Labels:**
- `address` (variable): The blockchain address of the wallet
- `account_name` (variable): The human-readable name assigned to the account
- `node_name` (constant): The name of the node configuration
- `module` (constant): The blockchain module type (`evm`, `cosmos`, or `erc20`)
- `exporter_version` (constant): The version of the exporter
- Custom labels (constant): Any additional labels defined in the node configuration

**Value:**
- `1.0` - Account is healthy, balance retrieved successfully
- `0.0` - Account is unhealthy, balance retrieval failed

**Example:**
```promql
# Check health of all accounts
blockchain_wallet_health

# Get unhealthy accounts
blockchain_wallet_health == 0

# Count healthy accounts per node
sum by (node_name) (blockchain_wallet_health)

# Alert on unhealthy account
blockchain_wallet_health{account_name="test-1-l2"} == 0

# Calculate availability percentage for a node
avg(blockchain_wallet_health{node_name="l2-gateway"}) * 100
```

**Notes:**
- Health checks are performed on every scrape
- A health value of `0` can indicate network issues, RPC node problems, or invalid addresses
- When health is `0`, the corresponding `blockchain_wallet_balance` metric will not be present for that scrape
- This metric is always exported regardless of health status, making it useful for monitoring availability

### 3. blockchain_wallet_scrapes_failed_total

**Type:** Counter

**Description:** Total number of scrape cycles where 50% or more accounts failed due to network errors. This metric helps identify when a blockchain node becomes unreachable or degraded.

**Labels:**
- `node_name` (constant): The name of the node configuration
- `module` (constant): The blockchain module type (`evm`, `cosmos`, or `erc20`)
- `exporter_version` (constant): The version of the exporter
- Custom labels (constant): Any additional labels defined in the node configuration

**Value:** Cumulative count of failed scrape cycles since the exporter started.

**Example:**
```promql
# Get total failed scrapes per node
blockchain_wallet_scrapes_failed_total

# Calculate rate of failed scrapes over 5 minutes
rate(blockchain_wallet_scrapes_failed_total[5m])

# Alert on repeated scrape failures
rate(blockchain_wallet_scrapes_failed_total{node_name="l2-gateway"}[5m]) > 0

# Count total failures across all nodes
sum(blockchain_wallet_scrapes_failed_total)

# Check if specific node has had any failures
blockchain_wallet_scrapes_failed_total{node_name="l1-host"} > 0
```

**Notes:**
- This counter only increments when ≥50% of accounts fail in a single scrape cycle
- Individual account failures (when <50% of accounts fail) do not increment this counter
- Useful for detecting node-level issues vs. account-specific problems
- The threshold logic helps distinguish between node outages and isolated account issues

## Label Reference

### Variable Labels

These labels vary per account and are included in metrics that track individual accounts:

| Label | Description | Example Values |
|-------|-------------|----------------|
| `address` | Blockchain address of the wallet | `0x5Fadfae7fDCa560e377FF26F26760891251De983`, `cosmos1...` |
| `account_name` | Human-readable account identifier from config | `test-1-l2`, `relayer`, `validator-1` |

### Constant Labels

These labels are constant for a given node configuration and are included in all metrics:

| Label | Description | Example Values |
|-------|-------------|----------------|
| `node_name` | Node configuration name | `l2-gateway`, `l1-host`, `kms-1` |
| `module` | Blockchain module type | `evm`, `cosmos`, `erc20` |
| `unit` | Balance unit for the metric | `eth`, `wei`, `gwei`, `cosm`, `ucosm`, `usdt`, `usdc` |
| `exporter_version` | Version of the exporter | `0.1.0`, `0.2.0` |

### Custom Labels

Additional labels can be defined in the node configuration under the `labels` section:

```yaml
nodes:
  - name: l2-gateway
    labels:
      app: l2-gateway
      env: production
      team: blockchain
      region: us-east
```

Custom labels are included as constant labels in all metrics for that node.

## Unit Handling

The exporter supports flexible unit configuration and automatic conversion:

### EVM (Ethereum) Module

- **Native Unit:** `wei` (blockchain's atomic unit, 10^18 wei = 1 ETH)
- **Common Metrics Units:** `eth`, `gwei`, `wei`
- **Configuration:**
  ```yaml
  unit: wei          # Native blockchain unit
  metricsUnit: eth   # Human-readable unit for metrics
  ```

### Cosmos Module

- **Native Unit:** `ucosm` or similar (10^6 ucosm = 1 COSM)
- **Common Metrics Units:** `cosm`, `ucosm`
- **Configuration:**
  ```yaml
  unit: ucosm        # Native blockchain unit
  metricsUnit: cosm  # Human-readable unit for metrics
  ```

### ERC20 Module

- **Native Unit:** Token-specific atomic unit (defined by token's `decimals`)
- **Auto-Discovery:** Automatically discovers `symbol`, `decimals`, and `name` from contract
- **Configuration with Auto-Discovery:**
  ```yaml
  autoUnitDiscovery: true  # Discovers units from contract
  ```
- **Manual Configuration:**
  ```yaml
  autoUnitDiscovery: false
  unit: usdt_atomic        # Atomic unit name
  metricsUnit: usdt        # Human-readable unit
  ```

## Common Query Patterns

### Balance Monitoring

```promql
# Current balance of all accounts
blockchain_wallet_balance

# Balances below threshold (for alerting)
blockchain_wallet_balance < 1.0

# Percentage of target balance
(blockchain_wallet_balance / 10) * 100

# Balance change rate over time
rate(blockchain_wallet_balance[5m])
```

### Health Monitoring

```promql
# Current health status of all accounts
blockchain_wallet_health

# Availability percentage per node
avg by (node_name) (blockchain_wallet_health) * 100

# Count of healthy accounts
count(blockchain_wallet_health == 1)

# Alert on unhealthy account for more than 5 minutes
blockchain_wallet_health == 0
```

### Scrape Failures

```promql
# Total failures per node
blockchain_wallet_scrapes_failed_total

# Failure rate over time
rate(blockchain_wallet_scrapes_failed_total[10m])

# Nodes with recent failures
increase(blockchain_wallet_scrapes_failed_total[1h]) > 0
```

### Multi-Node Aggregation

```promql
# Total balance across all EVM nodes
sum by (unit) (blockchain_wallet_balance{module="evm"})

# Average balance per application
avg by (app) (blockchain_wallet_balance)

# Count accounts per module
count by (module) (blockchain_wallet_balance)
```

### Environment-Specific Queries

```promql
# Production balances only
blockchain_wallet_balance{env="production"}

# Development environment health
blockchain_wallet_health{env="development"}

# Test environment failures
blockchain_wallet_scrapes_failed_total{env="test"}
```

## Recording Rules

Recommended Prometheus recording rules for common queries:

```yaml
groups:
  - name: blockchain_wallet_rules
    interval: 30s
    rules:
      # Scrape failure rate over 5 minutes
      - record: blockchain_wallet:scrape_failures:rate5m
        expr: rate(blockchain_wallet_scrapes_failed_total[5m])
```

## Alert Rules

Recommended Prometheus alert rules:

```yaml
groups:
  - name: blockchain_wallet_alerts
    rules:
      # Alert when account balance falls below threshold
      - alert: WalletBalanceLow
        expr: blockchain_wallet_balance < 0.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Wallet balance low for {{ $labels.account_name }}"
          description: "Account {{ $labels.account_name }} on {{ $labels.node_name }} has balance {{ $value }} {{ $labels.unit }}, which is below 0.5"

      # Alert when account balance is critically low
      - alert: WalletBalanceCritical
        expr: blockchain_wallet_balance < 0.1
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Wallet balance critical for {{ $labels.account_name }}"
          description: "Account {{ $labels.account_name }} on {{ $labels.node_name }} has balance {{ $value }} {{ $labels.unit }}, which is critically low"

      # Alert when account health check fails
      - alert: WalletUnhealthy
        expr: blockchain_wallet_health == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Wallet unhealthy for {{ $labels.account_name }}"
          description: "Account {{ $labels.account_name }} on {{ $labels.node_name }} has failed health checks for 5 minutes"

      # Alert when node has repeated scrape failures
      - alert: NodeScrapingFailing
        expr: rate(blockchain_wallet_scrapes_failed_total[10m]) > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Node {{ $labels.node_name }} scraping failing"
          description: "Node {{ $labels.node_name }} has had repeated scrape failures, indicating node connectivity issues"

      # Alert when no metrics received (exporter down)
      - alert: WalletExporterDown
        expr: up{job="blockchain-wallet-exporter"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Blockchain Wallet Exporter is down"
          description: "The blockchain wallet exporter has been down for more than 2 minutes"
```

## Grafana Dashboard Examples

### Single Stat Panel - Current Balance

```promql
blockchain_wallet_balance{account_name="test-1-l2"}
```

### Graph Panel - Balance Over Time

```promql
blockchain_wallet_balance{node_name="l2-gateway"}
```

### Table Panel - All Accounts Status

```promql
blockchain_wallet_balance * on(address, account_name) group_left blockchain_wallet_health
```


## Auto-Refund Feature (Non-Metric)

The auto-refund feature automatically maintains wallet balances but does not export separate Prometheus metrics. The refund operations can be observed through:

1. **Balance increases** in `blockchain_wallet_balance` after refunds
2. **Log messages** with the `[faucet]` and `[scheduler]` prefixes
3. **Transaction hashes** logged when refunds are confirmed

Example log entries:
```
[scheduler l2-gateway] Account test-1-l2 balance 0.300000 eth is below threshold 0.800000 eth, refunding...
[faucet l2-gateway] Successfully confirmed faucet claim for address 0x... with tx: 0xabcd...
[scheduler l2-gateway] Successfully refunded account test-1-l2 with 0.900000 eth
```

To monitor auto-refund operations:
- Track balance increases in Grafana: `increase(blockchain_wallet_balance[5m])`
- Monitor logs for faucet operations
- Set up log-based metrics or alerts in your logging system (e.g., Loki, Elasticsearch)

## Collection Behavior

### Concurrent Collection

- The exporter uses a worker pool with a default concurrency of 10 requests
- Account balances are collected concurrently for optimal performance
- Each scrape is independent and doesn't affect other metrics

### Timeout and Retries

- Default collection timeout: 10 seconds per account
- Failed collections result in `health=0` but don't block other accounts
- The scrape failure counter increments only when ≥50% of accounts fail

### Scrape Interval

The scrape interval is controlled by Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'blockchain-wallet-exporter'
    scrape_interval: 30s      # How often to scrape metrics
    scrape_timeout: 25s       # Timeout for the scrape
    static_configs:
      - targets: ['localhost:9091']
```

**Recommendations:**
- **Development:** 30s-1m scrape interval
- **Production:** 1m-5m scrape interval
- **High-frequency trading:** 15s-30s scrape interval
- Ensure `scrape_timeout` < `scrape_interval` to avoid overlapping scrapes

## Troubleshooting

### Missing Balance Metrics

If `blockchain_wallet_balance` is missing but `blockchain_wallet_health` shows:

1. **Health = 0**: Account is failing health checks
   - Check RPC endpoint connectivity
   - Verify address format and validity
   - Review exporter logs for error details

2. **Health = 1**: Should not occur (potential bug)
   - Check exporter logs
   - Verify Prometheus scrape configuration

### High Scrape Failures

If `blockchain_wallet_scrapes_failed_total` is increasing:

1. **Network Issues**: Check RPC endpoint availability
2. **Node Degradation**: Verify blockchain node health
3. **Rate Limiting**: Reduce scrape frequency or increase timeout
4. **SSL Issues**: Verify `httpSSLVerify` setting matches endpoint requirements

### Unexpected Balance Values

1. **Unit Mismatch**: Verify `unit` and `metricsUnit` configuration
2. **ERC20 Decimals**: Check `autoUnitDiscovery` or manual unit configuration
3. **Conversion Errors**: Review exporter logs for conversion warnings


