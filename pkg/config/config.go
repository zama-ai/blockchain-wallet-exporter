package config

import (
	"fmt"
	"io"
	"os"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"gopkg.in/yaml.v2"
)

type Schema struct {
	Global Global  `yaml:"global"`
	Nodes  []*Node `yaml:"nodes"`
}

type Global struct {
	Environment string `yaml:"environment"`
	MetricsAddr string `yaml:"metricsAddr"`
	LogLevel    string `yaml:"logLevel"`

	// Auto-refund scheduler configuration
	AutoRefund *AutoRefund `yaml:"autoRefund"`

	// TODO: add ssl config
	SSL *SSL `yaml:"ssl"`
}

// AutoRefund configuration for the scheduler
type AutoRefund struct {
	Enabled      bool   `yaml:"enabled"`
	Schedule     string `yaml:"schedule"` // Cron expression, e.g., "*/5 * * * *" for every 5 minutes
	FaucetURL    string `yaml:"faucetUrl"`
	FaucetURLEnv string `yaml:"faucetUrlEnv"`
	Timeout      int    `yaml:"timeout"` // Timeout in seconds, default 30
}

// TODO: add ssl config
type SSL struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

type Node struct {
	Name          string            `yaml:"name"`
	Module        string            `yaml:"module"`
	GrpcAddr      string            `yaml:"grpcAddr"`
	HttpAddr      string            `yaml:"httpAddr"`
	HttpAddrEnv   string            `yaml:"httpAddrEnv"`
	MetricsUnit   *currency.Unit    `yaml:"metricsUnit"`
	Unit          *currency.Unit    `yaml:"unit"`
	HttpSSLVerify string            `yaml:"httpSSLVerify"`
	GrpcSSLVerify string            `yaml:"grpcSSLVerify"`
	Accounts      []*Account        `yaml:"accounts"`
	Labels        map[string]string `yaml:"labels"`
	Authorization *Authorization    `yaml:"authorization"`
}

func (s *Schema) Normalize() error {
	// Normalize global auto-refund settings
	if s.Global.AutoRefund != nil {
		if err := s.Global.AutoRefund.Normalize(); err != nil {
			return fmt.Errorf("failed to normalize auto-refund config: %w", err)
		}
	}

	for _, node := range s.Nodes {
		if err := node.Normalize(); err != nil {
			return fmt.Errorf("failed to normalize node %s: %w", node.Name, err)
		}
		for _, acc := range node.Accounts {
			if err := acc.Normalize(); err != nil {
				return fmt.Errorf("failed to normalize account %s for node %s: %w", acc.Name, node.Name, err)
			}
		}
	}
	return nil
}

func (n *Node) Normalize() error {
	if n.MetricsUnit == nil {
		n.MetricsUnit = n.Unit
	}
	if n.HttpAddrEnv != "" {
		envValue := os.Getenv(n.HttpAddrEnv)
		if envValue != "" {
			n.HttpAddr = envValue
		}
	}
	return nil
}

type Authorization struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Account struct {
	Address    string `yaml:"address"`
	AddressEnv string `yaml:"addressEnv"`
	Name       string `yaml:"name"`

	// Auto-refund configuration for this account
	RefundThreshold *float64 `yaml:"refundThreshold"` // Balance threshold to trigger refund
	RefundTarget    *float64 `yaml:"refundTarget"`    // Target balance to reach after refund
}

func (a *Account) Normalize() error {
	if a.AddressEnv != "" {
		envValue := os.Getenv(a.AddressEnv)
		if envValue != "" {
			a.Address = envValue
		}
	}
	return nil
}

func (ar *AutoRefund) Normalize() error {
	if ar.FaucetURLEnv != "" {
		envValue := os.Getenv(ar.FaucetURLEnv)
		if envValue != "" {
			ar.FaucetURL = envValue
		}
	}
	if ar.Timeout == 0 {
		ar.Timeout = 30 // Default timeout of 30 seconds
	}
	if ar.Schedule == "" {
		ar.Schedule = "@every 30m" // Default to every 30 minutes
	}
	return nil
}

func NewConfig(path string) (*Schema, error) {
	cfg := &Schema{}

	return cfg, nil
}

func ReadConfigWithError(r io.Reader) (*Schema, error) {
	config := &Schema{}
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	if err := config.Normalize(); err != nil {
		return nil, fmt.Errorf("failed to normalize config: %w", err)
	}
	return config, nil
}
