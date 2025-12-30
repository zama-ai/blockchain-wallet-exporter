package httpfiber

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/gofiber/contrib/fiberzap/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/config"
	"github.com/zama-ai/blockchain-wallet-exporter/pkg/logger"

	"go.uber.org/zap"
)

type Server struct {
	app *fiber.App
	cfg *config.Schema

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
	logger.Infof("Stopping HTTP server...")
	if err := s.app.ShutdownWithTimeout(1 * time.Second); err != nil {
		logger.Debugf("HTTP server shutdown: %v", err)
	}
	logger.Infof("HTTP server stopped")
}

func (s *Server) MapRoutes() error {
	v1 := s.app.Group("/")
	v1.Get("/metrics", adaptor.HTTPHandler(promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{
		ErrorLog:      log.New(os.Stderr, log.Prefix(), log.Flags()),
		ErrorHandling: promhttp.ContinueOnError,
	})))
	return nil
}
