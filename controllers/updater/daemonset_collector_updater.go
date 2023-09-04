package updater

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math"
	"strconv"
)

func (dscu *DaemonSetCollectorUpdater) UpdateResources(metricsCount int, isUsingApplicationMetrics bool) error {
	var memory string
	var cpu string

	if !isUsingApplicationMetrics {
		memory = dscu.daemonSetConditions.Default.Values.Memory
		cpu = dscu.daemonSetConditions.Default.Values.Cpu
	} else {
		for _, daemonSetCondition := range dscu.daemonSetConditions.Conditions {
			if daemonSetCondition.MetricsCount >= metricsCount {
				memory = daemonSetCondition.Values.Memory
				cpu = daemonSetCondition.Values.Cpu
				break
			}
		}

		// neither state
		if memory == "" && cpu == "" {
			currentMemoryStr := dscu.absCollectorUpdater.collectorResources["limits"].(map[string]interface{})["memory"].(string)
			currentCpuStr := dscu.absCollectorUpdater.collectorResources["requests"].(map[string]interface{})["cpu"].(string)

			currentMemoryInt, err := strconv.Atoi(currentMemoryStr[:len(currentCpuStr)-2])
			if err != nil {
				return fmt.Errorf("error getting daemonset collector current memory integer: %w", err)
			}
			currentCpuInt, err := strconv.Atoi(currentCpuStr[:len(currentCpuStr)-1])
			if err != nil {
				return fmt.Errorf("error getting daemonset collector current cpu integer: %w", err)
			}

			addMemory := currentMemoryInt * 35 / 100 * int(math.Ceil(float64((currentMemoryInt-150000)/50000)))
			addCpu := currentCpuInt * 25 / 100 * int(math.Ceil(float64((currentCpuInt-150000)/50000)))
			memory = fmt.Sprintf("%dMi", currentMemoryInt+addMemory)
			cpu = fmt.Sprintf("%dm", currentCpuInt+addCpu)
		}
	}

	updatedCollectorResources, err := dscu.absCollectorUpdater.getUpdatedCollectorResources(memory, cpu)
	if err != nil {
		return fmt.Errorf("error updating resources: %w", err)
	}
	if updatedCollectorResources == nil {
		logger.Info("Daemonset collector resources were not changed", "currentMemory", memory, "currentCpu", cpu)
		return nil
	}

	dscu.absCollectorUpdater.collectorContainer.Resources = *updatedCollectorResources
	dscu.daemonSetCollector.Spec.Template.Spec.Containers[dscu.absCollectorUpdater.collectorContainerIndex] = *dscu.absCollectorUpdater.collectorContainer
	dscu.daemonSetCollector.CreationTimestamp = metav1.Now()
	dscu.daemonSetCollector.UID = ""
	dscu.daemonSetCollector.ResourceVersion = ""
	dscu.daemonSetCollector.SetSelfLink("")

	if _, err = dscu.daemonSetClient.Update(dscu.absCollectorUpdater.ctx, dscu.daemonSetCollector, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("error updating daemonset collector: %w", err)
	}

	logger.Info("Daemonset collector resources were updated", "memory", memory, "cpu", cpu)
	return nil
}
