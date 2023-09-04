package metrics_counter

import (
	"github.com/logzio/kubernetes-monitoring-operator/controllers/collector"
)

func (scmc *StandaloneCollectorMetricsCounter) GetMetricsCount(receiverConfigBuilder collector.ReceiverConfigBuilder, collec collector.Collector) (int, error) {
	exporterPort := "1234"
	receiverConfig, err := receiverConfigBuilder.BuildReceiverConfig(
		collector.WithKeepRelabelConfigsOnly())
	if err != nil {
		logger.Error(err, "Failed to build prometheus receiver config")
		return 0, err
	}

	logger.Info("Prometheus receiver config", "config", receiverConfig)

	exporter, err := collec.CreateExporter(exporterPort)
	if err != nil {
		logger.Error(err, "Failed to create exporter")
		return 0, err
	}

	receiver, err := collec.CreateReceiver(exporter, receiverConfig)
	if err != nil {
		logger.Error(err, "Failed to create receiver")
		return 0, err
	}

	defer collec.ShutdownCollector(receiver, exporter)

	if err = collec.StartCollector(receiver, exporter); err != nil {
		logger.Error(err, "Failed to start collector")
		return 0, err
	}

	metricsCount, err := collec.GetCollectedMetricsCount(exporterPort)
	if err != nil {
		logger.Error(err, "Failed to get collected metrics count")
		return 0, err
	}

	logger.Info("Metrics count", "metricsCount", metricsCount)
	return metricsCount, nil
}
