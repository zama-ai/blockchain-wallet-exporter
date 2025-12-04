package erc20

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
)

// Client encapsulates minimal interactions with an ERC20 contract.
type Client struct {
	node     *config.Node
	address  common.Address
	rpc      *rpc.Client
	eth      *ethclient.Client
	contract *bind.BoundContract
}

// Metadata represents ERC20 token metadata.
type Metadata struct {
	Name     string
	Symbol   string
	Decimals uint8
}

// NewClient creates a new ERC20 client for the provided node configuration.
func NewClient(ctx context.Context, node *config.Node) (*Client, error) {
	if node == nil {
		return nil, fmt.Errorf("node configuration cannot be nil")
	}
	if node.HttpAddr == "" {
		return nil, fmt.Errorf("node %s is missing httpAddr", node.Name)
	}
	if node.ContractAddr == "" {
		return nil, fmt.Errorf("node %s is missing contractAddress", node.Name)
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: strings.EqualFold(node.HttpSSLVerify, "false")}
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
		Timeout:   10 * time.Second,
	}

	opts := []rpc.ClientOption{rpc.WithHTTPClient(httpClient)}
	if node.Authorization != nil && node.Authorization.Username != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(node.Authorization.Username + ":" + node.Authorization.Password))
		opts = append(opts, rpc.WithHTTPAuth(func(h http.Header) error {
			h.Set("Authorization", fmt.Sprintf("Basic %s", creds))
			return nil
		}))
	}

	rpcClient, err := rpc.DialOptions(ctx, node.HttpAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rpc endpoint for node %s: %w", node.Name, err)
	}

	ethClient := ethclient.NewClient(rpcClient)
	address := common.HexToAddress(node.ContractAddr)
	contract := bind.NewBoundContract(address, erc20ABI, ethClient, ethClient, ethClient)

	return &Client{
		node:     node,
		address:  address,
		rpc:      rpcClient,
		eth:      ethClient,
		contract: contract,
	}, nil
}

// Close releases the underlying RPC resources.
func (c *Client) Close() {
	if c == nil {
		return
	}
	if c.eth != nil {
		c.eth.Close()
	}
	if c.rpc != nil {
		c.rpc.Close()
	}
}

// BalanceOf returns the raw token balance for the provided address.
func (c *Client) BalanceOf(ctx context.Context, address common.Address) (*big.Int, error) {
	out, err := c.call(ctx, "balanceOf", address)
	if err != nil {
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("balanceOf returned empty result")
	}
	value := abi.ConvertType(out[0], new(big.Int)).(*big.Int)
	return value, nil
}

// Decimals fetches the token decimals.
func (c *Client) Decimals(ctx context.Context) (uint8, error) {
	out, err := c.call(ctx, "decimals")
	if err != nil {
		return 0, fmt.Errorf("failed to call decimals: %w", err)
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("decimals returned empty result")
	}
	value := abi.ConvertType(out[0], new(uint8)).(*uint8)
	return *value, nil
}

// Symbol fetches the token symbol.
func (c *Client) Symbol(ctx context.Context) (string, error) {
	out, err := c.call(ctx, "symbol")
	if err != nil {
		return "", fmt.Errorf("failed to call symbol: %w", err)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("symbol returned empty result")
	}
	value := abi.ConvertType(out[0], new(string)).(*string)
	return *value, nil
}

// Name fetches the token name.
func (c *Client) Name(ctx context.Context) (string, error) {
	out, err := c.call(ctx, "name")
	if err != nil {
		return "", fmt.Errorf("failed to call name: %w", err)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("name returned empty result")
	}
	value := abi.ConvertType(out[0], new(string)).(*string)
	return *value, nil
}

// Metadata fetches symbol, name, and decimals in a single helper.
// Per ERC20 spec, name() is optional. If it fails, we fall back to using symbol as name.
// symbol() and decimals() are required for proper operation.
func (c *Client) Metadata(ctx context.Context) (*Metadata, error) {
	// symbol() is required for unit naming
	symbol, err := c.Symbol(ctx)
	if err != nil {
		return nil, fmt.Errorf("symbol is required: %w", err)
	}

	// decimals() is required for proper conversion
	decimals, err := c.Decimals(ctx)
	if err != nil {
		return nil, fmt.Errorf("decimals is required: %w", err)
	}

	// name() is optional per ERC20 spec, fall back to symbol if unavailable
	name, err := c.Name(ctx)
	if err != nil {
		name = symbol // Use symbol as fallback
	}

	return &Metadata{
		Name:     name,
		Symbol:   symbol,
		Decimals: decimals,
	}, nil
}

func (c *Client) call(ctx context.Context, method string, params ...any) ([]any, error) {
	var out []any
	if err := c.contract.Call(&bind.CallOpts{Context: ctx}, &out, method, params...); err != nil {
		return nil, err
	}
	return out, nil
}
