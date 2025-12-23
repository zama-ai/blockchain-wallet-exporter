# Blockchain Wallet Exporter Helm Chart

A Helm chart for deploying the Blockchain Wallet Exporter on Kubernetes. This exporter monitors wallet balances across EVM, Cosmos, and ERC20 networks and exports metrics to Prometheus.

## TL;DR

```bash
# Install from GitHub Container Registry (GHCR)
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/charts/blockchain-wallet-exporter \
  --version 0.1.2

# Or use the latest version
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/charts/blockchain-wallet-exporter
```

## Introduction

This chart bootstraps a Blockchain Wallet Exporter deployment on a Kubernetes cluster using the Helm package manager.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.8+ (with OCI support)
- PV provisioner support in the underlying infrastructure (if persistence is enabled)

## Installing the Chart

### From GHCR (Recommended)

```bash
# Install with default values
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.2

# Install with custom values
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.2 \
  --values custom-values.yaml

# Install in a specific namespace
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.0 \
  --namespace monitoring \
  --create-namespace
```

### From Local Source

```bash
# Clone the repository
git clone https://github.com/zama-ai/blockchain-wallet-exporter.git
cd blockchain-wallet-exporter

# Install from local chart
helm install my-blockchain-exporter ./charts/blockchain-wallet-exporter
```

## Uninstalling the Chart

```bash
helm uninstall my-blockchain-exporter
```

This command removes all Kubernetes resources associated with the chart.

## Configuration

### Key Configuration Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/zama-ai/blockchain-wallet-exporter/blockchain-wallet-exporter` |
| `image.tag` | Image tag | `""` (uses appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `9090` |
| `serviceMonitor.enabled` | Enable Prometheus ServiceMonitor | `false` |
| `ingress.enabled` | Enable ingress | `false` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.requests.cpu` | CPU request | `250m` |
| `resources.requests.memory` | Memory request | `256Mi` |

### Blockchain Configuration

The exporter requires wallet configuration through the `config` section. Here's an example:

```yaml
config:
  global:
    environment: production
    metricsAddr: ":9090"
    logLevel: info
    autoRefund:
      enabled: true
      schedule: "*/5 * * * *"
      faucetUrl: "https://faucet.example.com"
      timeout: 30

  nodes:
    - name: ethereum-mainnet
      module: evm
      httpAddr: "https://mainnet.infura.io/v3/YOUR-PROJECT-ID"
      # or use environment variable
      # httpAddrEnv: URL_WITH_API_KEY
      httpSSLVerify: true
      unit: wei
      metricsUnit: eth
      accounts:
        - address: "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb5"
          # or use environment variable
          # addressEnv: WALLET_ADDRESS
          name: wallet-1
          refundThreshold: 0.1
          refundTarget: 1.0
      labels:
        network: mainnet
        app: monitoring

    - name: cosmos-hub
      module: cosmos
      grpcAddr: "grpc://cosmos-grpc.example.com:9090"
      grpcSSLVerify: true
      unit: uatom
      metricsUnit: atom
      accounts:
        - address: "cosmos1..."
          name: validator-wallet
          refundThreshold: 100.0
          refundTarget: 1000.0
      labels:
        network: cosmos-hub
        app: validator
```

### Example Installation with Custom Config

```bash
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --version 0.1.0 \
  --set config.global.logLevel=debug \
  --set serviceMonitor.enabled=true \
  --values my-config.yaml
```

### Prometheus Integration

To enable automatic scraping by Prometheus Operator:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  scrapeTimeout: 10s
  labels:
    release: prometheus
```

Or using command line:

```bash
helm install my-blockchain-exporter \
  oci://ghcr.io/zama-ai/blockchain-wallet-exporter/charts/blockchain-wallet-exporter \
  --set serviceMonitor.enabled=true \
  --set serviceMonitor.interval=30s
```

## Version Management

This chart follows semantic versioning:

- **MAJOR** version: Breaking changes to chart API or required values
- **MINOR** version: New features, new optional values
- **PATCH** version: Bug fixes, documentation updates

Always check the [CHANGELOG](../../CHANGELOG.md) before upgrading.

## Advanced Configuration

### Using Secrets for Sensitive Data

For production deployments, store sensitive data (API keys, passwords) in Kubernetes Secrets:

```yaml
# Create a secret
apiVersion: v1
kind: Secret
metadata:
  name: blockchain-exporter-secrets
type: Opaque
stringData:
  infura-api-key: "YOUR-API-KEY"
  faucet-api-key: "YOUR-FAUCET-KEY"
```

Then reference it in your values:

```yaml
envFrom:
  - secretRef:
      name: blockchain-exporter-secrets
```

### Resource Limits

Adjust resource limits based on the number of wallets being monitored:

```yaml
resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 1000m
    memory: 1Gi
```

### Multiple Instances

To run multiple instances with different configurations:

```bash
# Instance 1: Ethereum wallets
helm install eth-exporter \
  oci://ghcr.io/zama-ai/charts/blockchain-wallet-exporter \
  --values eth-config.yaml

# Instance 2: Cosmos wallets
helm install cosmos-exporter \
  oci://ghcr.io/zama-ai/charts/blockchain-wallet-exporter \
  --values cosmos-config.yaml
```

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -l app.kubernetes.io/name=blockchain-wallet-exporter
```

### View Logs

```bash
kubectl logs -l app.kubernetes.io/name=blockchain-wallet-exporter -f
```

### Verify Configuration

```bash
helm get values my-blockchain-exporter
```

### Test Metrics Endpoint

```bash
kubectl port-forward svc/my-blockchain-exporter 9090:9090
curl http://localhost:9090/metrics
```

## Support

For issues and feature requests, please visit:
- **GitHub Issues**: https://github.com/zama-ai/blockchain-wallet-exporter/issues
- **Documentation**: https://github.com/zama-ai/blockchain-wallet-exporter

## License

See [LICENSE](../../LICENSE) file for details.
