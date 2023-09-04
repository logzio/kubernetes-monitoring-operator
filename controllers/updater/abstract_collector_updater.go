package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

func newAbstractCollectorUpdater(ctx context.Context) *abstractCollectorUpdater {
	if logger == nil {
		newLogger := log.FromContext(ctx)
		logger = &newLogger
	}

	resourcesConditionsConfigPath := os.Getenv(resourcesConditionsConfigPathEnv)
	if resourcesConditionsConfigPath == "" {
		resourcesConditionsConfigPath = defaultResourcesConditionsConfigPath
	}

	return &abstractCollectorUpdater{
		ctx:                           ctx,
		resourcesConditionsConfigPath: resourcesConditionsConfigPath,
	}
}

func (acu *abstractCollectorUpdater) setCollectorContainer(collectorContainers []corev1.Container, collectorContainerName string) error {
	for collectorContainerIndex, collectorContainer := range collectorContainers {
		if collectorContainer.Name == collectorContainerName {
			acu.collectorContainer = &collectorContainer
			acu.collectorContainerIndex = collectorContainerIndex
			break
		}
	}

	if acu.collectorContainer == nil {
		return fmt.Errorf("collector container '%s' was not found", acu.collectorContainerName)
	}

	return nil
}

func (acu *abstractCollectorUpdater) getResourcesConfigData() (*resourcesConfig, error) {
	configFile, err := os.ReadFile(acu.resourcesConditionsConfigPath)
	if err != nil {
		return nil, fmt.Errorf("error reading resources config file '%s': %w", acu.resourcesConditionsConfigPath, err)
	}

	var config resourcesConfig
	if err = yaml.Unmarshal(configFile, &config); err != nil {
		return nil, fmt.Errorf("error unmarshaling resources config file: %w", err)
	}

	return &config, nil
}

func (acu *abstractCollectorUpdater) setCollectorResources() error {
	collectorResourcesBytes, err := json.Marshal(acu.collectorContainer.Resources)
	if err != nil {
		return fmt.Errorf("error marshaling collector resources: %w", err)
	}

	collectorResourcesMap := make(map[string]interface{}, 0)
	if err = json.Unmarshal(collectorResourcesBytes, &collectorResourcesMap); err != nil {
		return fmt.Errorf("error unmarshaling collector resources: %w", err)
	}

	limits, ok := collectorResourcesMap["limits"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("resources does not have limits")
	}
	_, ok = limits["memory"].(string)
	if !ok {
		return fmt.Errorf("resources limits does not have memory")
	}

	requests, ok := collectorResourcesMap["requests"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("resources does not have requests")
	}
	_, ok = requests["cpu"].(string)
	if !ok {
		return fmt.Errorf("requests does not have cpu")
	}

	acu.collectorResources = collectorResourcesMap
	return nil
}

func (acu *abstractCollectorUpdater) getUpdatedCollectorResources(memory string, cpu string) (*corev1.ResourceRequirements, error) {
	currentMemory := acu.collectorResources["limits"].(map[string]interface{})["memory"].(string)
	currentCpu := acu.collectorResources["requests"].(map[string]interface{})["cpu"].(string)

	if currentMemory == memory && currentCpu == cpu {
		return nil, nil
	}

	acu.collectorResources["limits"].(map[string]interface{})["memory"] = memory
	acu.collectorResources["requests"].(map[string]interface{})["cpu"] = cpu

	updatedCollectorContainerResourcesBytes, err := json.Marshal(acu.collectorResources)
	if err != nil {
		return nil, fmt.Errorf("error marshaling updated collector resources: %w", err)
	}

	var updatedCollectorResources corev1.ResourceRequirements
	if err = json.Unmarshal(updatedCollectorContainerResourcesBytes, &updatedCollectorResources); err != nil {
		return nil, fmt.Errorf("error unmarshaling updated collector resources: %w", err)
	}

	return &updatedCollectorResources, nil
}
