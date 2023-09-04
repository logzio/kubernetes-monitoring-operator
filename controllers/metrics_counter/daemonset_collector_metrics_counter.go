package metrics_counter

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/logzio/kubernetes-monitoring-operator/controllers/collector"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (dscmc *DaemonSetCollectorMetricsCounter) GetMetricsCount(receiverConfigBuilder collector.ReceiverConfigBuilder, collec collector.Collector) (int, error) {
	nodes, err := dscmc.absCollectorMetricsCounter.clientset.CoreV1().Nodes().List(dscmc.absCollectorMetricsCounter.ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("error listing all nodes in Kubernetes cluster: %w", err)
	}

	var wg sync.WaitGroup
	var mu = &sync.Mutex{}
	var metricsCounts = make([]int, 0)
	errChan := make(chan error, 1)
	ctx, cancel := context.WithCancel(dscmc.absCollectorMetricsCounter.ctx)
	defer cancel()
	currentWorkersNum := 0
	exporterPort := 1234

	for _, node := range nodes.Items {
		currentWorkersNum++
		wg.Add(1)
		go func(nodeName string, exporterPort string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			receiverConfig, err := receiverConfigBuilder.BuildReceiverConfig(
				collector.WithNode(nodeName),
				collector.WithKeepRelabelConfigsOnly())
			if err != nil {
				logger.Error(err, "Failed to build prometheus receiver config", "node", nodeName)
				errChan <- err
				cancel()
				return
			}

			logger.Info("Prometheus receiver config", "config", receiverConfig, "node", nodeName)

			exporter, err := collec.CreateExporter(exporterPort)
			if err != nil {
				logger.Error(err, "Failed to create exporter", "node", nodeName)
				errChan <- err
				cancel()
				return
			}

			receiver, err := collec.CreateReceiver(exporter, receiverConfig)
			if err != nil {
				logger.Error(err, "Failed to create receiver", "node", nodeName)
				errChan <- err
				cancel()
				return
			}

			defer collec.ShutdownCollector(receiver, exporter)

			if err = collec.StartCollector(receiver, exporter); err != nil {
				logger.Error(err, "Failed to start collector", "node", nodeName)
				errChan <- err
				cancel()
				return
			}

			metricsCount, err := collec.GetCollectedMetricsCount(exporterPort)
			if err != nil {
				logger.Error(err, "Failed to get collected metrics count", "node", nodeName)
				errChan <- err
				cancel()
				return
			}

			logger.Info("Metrics count", "metricsCounter", metricsCount, "node", nodeName)

			mu.Lock()
			metricsCounts = append(metricsCounts, metricsCount)
			mu.Unlock()
		}(node.Name, strconv.Itoa(exporterPort))

		if currentWorkersNum == dscmc.workersNum {
			wg.Wait()
			if ctx.Err() != nil {
				return 0, <-errChan
			}

			currentWorkersNum = 0
		}

		exporterPort++
	}

	wg.Wait()
	if ctx.Err() != nil {
		return 0, <-errChan
	}

	maxMetricsCount := 0
	for _, metricsCount := range metricsCounts {
		if metricsCount > maxMetricsCount {
			maxMetricsCount = metricsCount
		}
	}

	logger.Info("Max metrics count", "maxMetricsCounter", maxMetricsCount)
	return maxMetricsCount, nil
}
