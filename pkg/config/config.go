package config

import (
	"fmt"
	"io"

	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
	"gopkg.in/yaml.v2"
)

type Schema struct {
	Global Global `yaml:"global"`
	Nodes  []Node `yaml:"nodes"`
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
	Unit          *currency.Unit    `yaml:"unit"`
	HttpSSLVerify string            `yaml:"httpSSLVerify"`
	GrpcSSLVerify string            `yaml:"grpcSSLVerify"`
	Accounts      []*Account        `yaml:"accounts"`
	Labels        map[string]string `yaml:"labels"`
	Authorization *Authorization    `yaml:"authorization"`
}

type Authorization struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Account struct {
	Address string `yaml:"address"`
	Name    string `yaml:"name"`
}

func NewConfig(path string) (*Schema, error) {
	cfg := &Schema{}

	return cfg, nil
}

func ReadConfig(r io.Reader) *Schema {
	config := &Schema{}
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&config); err != nil {
		logger.Errorf("failed to decode config: %v", err)
		return nil
	}
	return config
}

func ReadConfigWithError(r io.Reader) (*Schema, error) {
	config := &Schema{}
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	return config, nil
}
