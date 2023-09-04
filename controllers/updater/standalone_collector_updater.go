package updater

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (scu *StandaloneCollectorUpdater) UpdateResources(metricsCount int, isUsingApplicationMetrics bool) error {
	var memory string
	var cpu string

	if !isUsingApplicationMetrics {
		memory = scu.standaloneConditions.Default.Values.Memory
		cpu = scu.standaloneConditions.Default.Values.Cpu
	} else {
		for _, standaloneCondition := range scu.standaloneConditions.Conditions {
			if standaloneCondition.MetricsCount >= metricsCount {
				memory = standaloneCondition.Values.Memory
				cpu = standaloneCondition.Values.Cpu
				break
			}
		}
	}

	updatedCollectorResources, err := scu.absCollectorUpdater.getUpdatedCollectorResources(memory, cpu)
	if err != nil {
		return fmt.Errorf("error updating resources: %w", err)
	}
	if updatedCollectorResources == nil {
		logger.Info("Standalone collector resources were not changed", "currentMemory", memory, "currentCpu", cpu)
		return nil
	}

	scu.absCollectorUpdater.collectorContainer.Resources = *updatedCollectorResources
	scu.standaloneCollector.Spec.Template.Spec.Containers[scu.absCollectorUpdater.collectorContainerIndex] = *scu.absCollectorUpdater.collectorContainer
	scu.standaloneCollector.CreationTimestamp = metav1.Now()
	scu.standaloneCollector.UID = ""
	scu.standaloneCollector.ResourceVersion = ""
	scu.standaloneCollector.SetSelfLink("")

	if _, err = scu.standaloneClient.Update(scu.absCollectorUpdater.ctx, scu.standaloneCollector, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("error updating standalone collector: %w", err)
	}

	logger.Info("Standalone collector resources were updated", "memory", memory, "cpu", cpu)
	return nil
}
