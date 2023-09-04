package collector

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	kitlog "github.com/go-kit/log"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/prometheusexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver"
	promconfig "github.com/prometheus/prometheus/config"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
)

// StartCollector starts the collector
func (pc *PrometheusCollector) StartCollector(receiver receiver.Metrics, exporter exporter.Metrics) error {
	// Start prometheus metrics exporter
	if err := exporter.Start(pc.ctx, nil); err != nil {
		return fmt.Errorf("error starting prometheus exporter: %w", err)
	}

	// Start prometheus metrics receiver
	if err := receiver.Start(pc.ctx, nil); err != nil {
		return fmt.Errorf("error starting prometheus receiver: %w", err)
	}

	return nil
}

// ShutdownCollector shuts down the collector
func (pc *PrometheusCollector) ShutdownCollector(receiver receiver.Metrics, exporter exporter.Metrics) {
	receiver.Shutdown(pc.ctx)
	exporter.Shutdown(pc.ctx)
}

// GetCollectedMetricsCount returns the number of collected metrics
func (pc *PrometheusCollector) GetCollectedMetricsCount(port string) (int, error) {
	var collectedMetrics string
	var err error

	for true {
		time.Sleep(time.Second * time.Duration(pc.collectorInterval+5))

		// Get collected metrics from prometheus exporter
		collectedMetrics, err = pc.getCollectedMetrics(port)
		if err != nil {
			return 0, fmt.Errorf("error getting collected metrics from otel: %w", err)
		}

		if collectedMetrics != "" {
			break
		}
	}

	//logger.Info("Collected metrics", "metrics", collectedMetrics)

	// Count metrics (each metric is a line)
	var metricsCounter = 0
	for _, metric := range strings.Split(collectedMetrics, "\n") {
		if metric == "" {
			continue
		}
		if strings.HasPrefix(metric, "#") {
			continue
		}

		metricsCounter++
	}

	return metricsCounter, nil
}

// CreateExporter creates a prometheus exporter
func (pc *PrometheusCollector) CreateExporter(port string) (exporter.Metrics, error) {
	exporterFactory := prometheusexporter.NewFactory()
	exporterConfig := exporterFactory.CreateDefaultConfig().(*prometheusexporter.Config)
	exporterConfig.Endpoint = fmt.Sprintf("0.0.0.0:%s", port)

	if err := exporterConfig.Validate(); err != nil {
		return nil, fmt.Errorf("prometheus exporter configuration is not valid: %w", err)
	}

	exporterSettings := exporter.CreateSettings{
		TelemetrySettings: component.TelemetrySettings{
			Logger:         pc.otelLogger,
			TracerProvider: otel.GetTracerProvider(),
		},
		BuildInfo: component.BuildInfo{
			Description: "Prometheus exporter",
			Version:     otel.Version(),
		},
	}
	metricsExporter, err := exporterFactory.CreateMetricsExporter(pc.ctx, exporterSettings, exporterConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus metrics exporter: %w", err)
	}

	return metricsExporter, nil
}

// CreateReceiver creates a prometheus exporter
func (pc *PrometheusCollector) CreateReceiver(exporter exporter.Metrics, config string) (receiver.Metrics, error) {
	promConfig, err := promconfig.Load(config, false, kitlog.NewNopLogger())
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus receiver configuration object: %w", err)
	}

	receiverConfig := &prometheusreceiver.Config{
		PrometheusConfig: promConfig,
	}

	if err = receiverConfig.Validate(); err != nil {
		return nil, fmt.Errorf("prometheus receiver configuration is not valid: %w", err)
	}

	receiverFactory := prometheusreceiver.NewFactory()
	receiverSettings := receiver.CreateSettings{
		TelemetrySettings: component.TelemetrySettings{
			Logger:         pc.otelLogger,
			TracerProvider: otel.GetTracerProvider(),
			MeterProvider:  noop.NewMeterProvider(),
		},
		BuildInfo: component.BuildInfo{
			Description: "Prometheus receiver",
			Version:     otel.Version(),
		},
	}
	metricsReceiver, err := receiverFactory.CreateMetricsReceiver(pc.ctx, receiverSettings, receiverConfig, exporter)
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus metrics receiver: %w", err)
	}

	return metricsReceiver, nil
}

// getCollectedMetrics returns the collected metrics
func (pc *PrometheusCollector) getCollectedMetrics(port string) (string, error) {
	url := fmt.Sprintf("http://0.0.0.0:%s/metrics", port)

	var client http.Client
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("error getting '%s' response: %w", url, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("'%s' response status code is %d", url, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading '%s' response body: %w", url, err)
	}

	return string(bodyBytes), nil
}
