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

	// TODO: add ssl config
	SSL *SSL `yaml:"ssl"`
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
	MetricsUnit   *currency.Unit    `yaml:"metricsUnit"`
	Unit          *currency.Unit    `yaml:"unit"`
	HttpSSLVerify string            `yaml:"httpSSLVerify"`
	GrpcSSLVerify string            `yaml:"grpcSSLVerify"`
	Accounts      []*Account        `yaml:"accounts"`
	Labels        map[string]string `yaml:"labels"`
	Authorization *Authorization    `yaml:"authorization"`
}

func (s *Schema) Normalize() error {
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
