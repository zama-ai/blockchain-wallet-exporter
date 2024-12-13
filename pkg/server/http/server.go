package httpfiber

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/gofiber/contrib/fiberzap/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/collector"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/currency"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/validation"

	ioPrometheusClient "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
)

type Server struct {
	app              *fiber.App
	cfg              *config.Schema
	currencyRegistry *currency.Registry

	// Max concurrent requests for the collector
	MaxConccurentRequests int

	registry *prometheus.Registry
}

type Option func(*Server)

func NewServer(cfg *config.Schema, opts ...Option) *Server {
	app := fiber.New(fiber.Config{
		JSONEncoder:           json.Marshal,
		JSONDecoder:           json.Unmarshal,
		DisableStartupMessage: true,
	})
	//ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{
		app: app,
		cfg: cfg,
	}
	for _, opt := range opts {
		opt(srv)
	}
	return srv
}

func WithRegistry(registry *prometheus.Registry) Option {
	return func(s *Server) {
		s.registry = registry
	}
}

func WithCurrencyRegistry(registry *currency.Registry) Option {
	return func(s *Server) {
		s.currencyRegistry = registry
	}
}

func (s *Server) ValidateConfig() error {
	validator := validation.NewConfigValidator()
	return validator.ValidateConfig(s.cfg)
}

func (s *Server) InitCollectors() error {
	var (
		prometheusCollector prometheus.Collector
		err                 error
	)

	for _, node := range s.cfg.Nodes {
		switch node.Module {
		case "evm":
			logger.Infof("initializing evm collector for %s", node.Name)
			prometheusCollector, err = collector.NewEVMCollector(node, s.currencyRegistry, collector.WithEVMLabels(node.Labels))
			if err != nil {
				return fmt.Errorf("failed to init evm collector: %v", err)
			}
		case "cosmos":
			logger.Infof("initializing cosmos collector for %s", node.Name)
			prometheusCollector, err = collector.NewCosmosCollector(node, s.currencyRegistry, collector.WithCosmosLabels(node.Labels))
			if err != nil {
				return fmt.Errorf("failed to init cosmos collector: %v", err)
			}
		default:
			return fmt.Errorf("invalid module: %s", node.Module)
		}
		err = s.registry.Register(prometheusCollector)
		if err != nil {
			return fmt.Errorf("failed to register collector: %v", err)
		}
	}
	return nil
}

func (s *Server) Run() error {
	if s.cfg.Global.Environment == "production" {
		// parse log level
		level, err := zap.ParseAtomicLevel(s.cfg.Global.LogLevel)
		if err != nil {
			return err
		}
		zapLogger, err := logger.NewZapLogger(logger.WithLevel(level.Level()))
		if err != nil {
			return err
		}
		s.app.Use(fiberzap.New(fiberzap.Config{
			Logger: zapLogger.Logger,
		}))
	}

	// validate config
	if err := s.ValidateConfig(); err != nil {
		return err
	}

	// init collector
	if err := s.InitCollectors(); err != nil {
		return err
	}

	// init routes
	if err := s.MapRoutes(); err != nil {
		logger.Fatalf("failed to map routes: %v", err)
	}

	// run health check for readiness
	s.app.Get("/readiness", func(c *fiber.Ctx) error {
		// TODO: Define health check for readiness
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"status": "ok",
		})
	})

	// init collector
	logger.Infof("listening on %s", s.cfg.Global.MetricsAddr)
	if err := s.app.Listen(s.cfg.Global.MetricsAddr); err != nil {
		return err
	}

	return nil
}

func (s *Server) Stop() {
	_ = s.app.Shutdown()
}

func (s *Server) MapRoutes() error {
	v1 := s.app.Group("/")
	v1.Get("/metrics", adaptor.HTTPHandler(promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		ErrorLog:      log.New(os.Stderr, log.Prefix(), log.Flags()),
		ErrorHandling: promhttp.ContinueOnError,
	})))

	v1.Get("/accounts", func(c *fiber.Ctx) error {
		address := c.Query("address")
		metrics, err := s.registry.Gather()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		var filteredMetrics []*ioPrometheusClient.MetricFamily
		for _, metric := range metrics {
			if metric.GetName() == "blockchain_wallet_balance" ||
				metric.GetName() == "blockchain_wallet_health" {

				if address != "" {
					filteredMetric := &ioPrometheusClient.MetricFamily{
						Name:   metric.Name,
						Help:   metric.Help,
						Type:   metric.Type,
						Metric: filterMetricsByAddress(metric.Metric, address),
					}
					if len(filteredMetric.Metric) > 0 {
						filteredMetrics = append(filteredMetrics, filteredMetric)
					}
				} else {
					filteredMetrics = append(filteredMetrics, metric)
				}
			}
		}

		if len(filteredMetrics) == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "No metrics found for the given address",
			})
		}

		return c.JSON(filteredMetrics)
	})
	return nil
}

// Helper function to filter metrics by address
func filterMetricsByAddress(metrics []*ioPrometheusClient.Metric, address string) []*ioPrometheusClient.Metric {
	var filtered []*ioPrometheusClient.Metric
	for _, m := range metrics {
		for _, label := range m.GetLabel() {
			if label.GetName() == "address" && label.GetValue() == address {
				filtered = append(filtered, m)
				break
			}
		}
	}
	return filtered
}
