# Blockchain Wallet Exporter

## Goals

- keep it minimal as possible
- Retrieve the balance of the wallet for different blockchains (cosmos, ethereum) and export them as metrics
- Should be able to retrieve the balance concurrently for multiple wallets (use existing worker pool for executing the tasks)
- Labels keys should be identical for all wallets 
- Should able to choose wei or eth as the unit of the balance (default should be eth), for cosmos it should be ucosm ? if it's eth we should convert it to the unit of the balance
- Use Golang for the exporter because it's easy to integrate with prometheus (made in golang)
- The exporter will get the metrics from the node rpc


# To Define

- Can't auto discover the wallets from the node rpc (the wallets should be configured manually) ????
- the project should or not oss ?
- license project ?
- should define the authorization for target node rpc ?

## Non Goals

- The exporter shouldn't pull metrics from validator
- The exporter run on single instance (no multi-instance support, or cluster support)
- for now, only support cosmos and ethereum
- only retrieve the balance of the wallet, not the transactions, etc
- only support grpc for cosmos and http for ethereum
- should not have a ssl metrics endpoint for now


# Time to Build

- 1-2 full days


## Spec

Use the following spec to configure the exporter

```yaml
# Config for the exporter

global:
  maxConcurrentRequests: 10
  environment: production
  metricsAddr: ":9090"
  logLevel: info
  #ssl:
  #  enabled: false
  #  certFile: ""
  #  keyFile: ""



# 4 connector
# 1 validator
# 1 gateway (fhevm)
# 1 gateway (kms)

nodes:
  - name: kms-1
    module: cosmos
    grpcAddr: "grpc://127.0.0.1:9090"
    #grpcAddress: "grpcs://127.0.0.1:9090" with SSL enabled
    grpcSSLVerify: false
    # unit for cosmos is ucosm or 
    unit: ucosm
    accounts:
      - address: "wasm..."
        name: wasm-1
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
    # unit for evm is eth or wei
    unit: eth
    accounts:
      - address: "0x02933E8678FE8F5D4CFFD0E331A264CD86AEBD8A"
        name: eth-2
    labels:
      app: fhevm
      env: test
```

### ERC20 modules

ERC20 token monitoring follows the same structure but additionally requires the target contract address. By default you can keep specifying `unit`/`metricsUnit` (useful when the registry already knows those units). When `autoUnitDiscovery: true` is set, the exporter will query the contract for `symbol`/`decimals`, register the corresponding base/metrics units automatically, and you can omit the unit fields entirely.

```yaml
  - name: l2-gateway-erc20
    module: erc20
    httpAddr: "https://l2-gateway-node-zws-dev-http.diplodocus-boa.ts.net"
    contractAddress: "0x0000000000000000000000000000000000000000"
    autoUnitDiscovery: true   # Let the exporter discover decimals/symbol and register units
    autoRefund:
      enabled: true
      schedule: "@every 1m"
      faucetUrl: "http://localhost:8080"
      timeout: 30
    accounts:
      - address: "0xfcD843C3Bf1Dd5Aa3Bd78cdD3e0E28A34ef2fDaE"
        name: test-1-l2
        refundThreshold: 0.8
        refundTarget: 1.2
```

