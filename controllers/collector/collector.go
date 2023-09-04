package collector

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	receiverScrapeIntervalEnv = "RECEIVER_SCRAPE_INTERVAL"

	defaultReceiverScrapeInterval = "5"
)

var (
	// logger for the entire package
	logger *logr.Logger = nil
)

// Collector is an interface for running collector
type Collector interface {
	CreateExporter(port string) (exporter.Metrics, error)
	CreateReceiver(exporter exporter.Metrics, config string) (receiver.Metrics, error)
	StartCollector(receiver.Metrics, exporter.Metrics) error
	ShutdownCollector(receiver.Metrics, exporter.Metrics)
	GetCollectedMetricsCount(port string) (int, error)
}

// PrometheusCollector is a metrics collector using prometheus receiver and exporter
type PrometheusCollector struct {
	ctx               context.Context
	otelLogger        *zap.Logger
	collectorInterval int
}

// ReceiverConfigBuilder is an interface for building receiver config
type ReceiverConfigBuilder interface {
	BuildReceiverConfig(...ReceiverConfigBuilderOption) (string, error)
	IsApplicationMetricsEnabled() (bool, error)
}

// ReceiverConfigBuilderOption is an option that tells how to build the receiver config
type ReceiverConfigBuilderOption func(map[string]interface{}) error

// PrometheusReceiverConfigBuilder builds prometheus receiver config using the collector ConfigMap
type PrometheusReceiverConfigBuilder struct {
	ctx                          context.Context
	configMapClient              corev1.ConfigMapInterface
	configMapCollectorData       map[string]interface{}
	receiverScrapeInterval       string
	configMapCollectorNameSuffix string
}

// prometheusReceiverConfig represents a prometheus receiver config
type prometheusReceiverConfig struct {
	Global        map[string]string `json:"global"`
	ScrapeConfigs []interface{}     `json:"scrape_configs"`
}

// NewPrometheusCollector creates a new metrics collector
func NewPrometheusCollector(ctx context.Context) (*PrometheusCollector, error) {
	if logger == nil {
		newLogger := log.FromContext(ctx)
		logger = &newLogger
	}

	receiverScrapeInterval := os.Getenv(receiverScrapeIntervalEnv)
	if receiverScrapeInterval == "" {
		receiverScrapeInterval = defaultReceiverScrapeInterval
	}

	collectorInterval, err := strconv.Atoi(receiverScrapeInterval)
	if err != nil {
		return nil, fmt.Errorf("error converting receiver scrape interval to integer")
	}

	// Create otel logger
	otelLogger, err := zap.NewProduction()
	if err != nil {
		logger.Info("Failed to create otel logger. Creating no-op logger instead", "error", err)
		otelLogger = zap.NewNop()
	}

	collector := &PrometheusCollector{
		ctx:               ctx,
		otelLogger:        otelLogger,
		collectorInterval: collectorInterval,
	}

	return collector, nil
}

// NewPrometheusReceiverConfigBuilder creates a new PrometheusReceiverConfigBuilder
func NewPrometheusReceiverConfigBuilder(ctx context.Context, configMapClient corev1.ConfigMapInterface, configMapCollectorNameSuffix string) (*PrometheusReceiverConfigBuilder, error) {
	if logger == nil {
		newLogger := log.FromContext(ctx)
		logger = &newLogger
	}

	receiverScrapeInterval := os.Getenv(receiverScrapeIntervalEnv)
	if receiverScrapeInterval == "" {
		receiverScrapeInterval = defaultReceiverScrapeInterval
	}

	configMapCollectorData, err := getConfigMapCollectorData(ctx, configMapClient, configMapCollectorNameSuffix)
	if err != nil {
		return nil, fmt.Errorf("error getting the ConfigMap collector data: %w", err)
	}

	return &PrometheusReceiverConfigBuilder{
		ctx:                          ctx,
		configMapClient:              configMapClient,
		configMapCollectorData:       configMapCollectorData,
		receiverScrapeInterval:       receiverScrapeInterval,
		configMapCollectorNameSuffix: configMapCollectorNameSuffix,
	}, nil
}
