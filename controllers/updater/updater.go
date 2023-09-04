package updater

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	resourcesConditionsConfigPathEnv    = "RESOURCES_CONDITIONS_CONFIG_PATH"
	daemonSetCollectorContainerNameEnv  = "DAEMONSET_COLLECTOR_CONTAINER_NAME"
	standaloneCollectorContainerNameEnv = "STANDALONE_COLLECTOR_CONTAINER_NAME"

	defaultResourcesConditionsConfigPath    = "/etc/configs/resources_conditions_config.yaml"
	defaultDaemonSetCollectorContainerName  = "logzio-k8s-telemetry"
	defaultStandaloneCollectorContainerName = "logzio-k8s-telemetry"
)

var (
	// logger for the entire package
	logger *logr.Logger = nil
)

type CollectorModeUpdater interface {
	UpdateResources(metricsCount int, isUsingApplicationMetrics bool) error
}

type DaemonSetCollectorUpdater struct {
	absCollectorUpdater *abstractCollectorUpdater
	daemonSetCollector  *appsv1.DaemonSet
	daemonSetClient     typedappsv1.DaemonSetInterface
	daemonSetConditions *collectorModeConditions
}

type StandaloneCollectorUpdater struct {
	absCollectorUpdater  *abstractCollectorUpdater
	standaloneCollector  *appsv1.Deployment
	standaloneClient     typedappsv1.DeploymentInterface
	standaloneConditions *collectorModeConditions
}

type abstractCollectorUpdater struct {
	ctx                           context.Context
	collectorContainerName        string
	collectorContainer            *corev1.Container
	collectorContainerIndex       int
	collectorResources            map[string]interface{}
	resourcesConditionsConfigPath string
}

type resourcesConfig struct {
	DaemonSet  collectorModeConditions `json:"daemonset"`
	Standalone collectorModeConditions `json:"standalone"`
}

type collectorModeConditions struct {
	Default    defaultCondition `json:"default"`
	Conditions []condition      `json:"conditions"`
}

type defaultCondition struct {
	Values values `json:"values"`
}

type condition struct {
	MetricsCount int    `json:"metrics_count"`
	Values       values `json:"values"`
}

type values struct {
	Memory string `json:"memory"`
	Cpu    string `json:"cpu"`
}

func NewDaemonSetCollectorUpdater(ctx context.Context, daemonSetCollector *appsv1.DaemonSet, daemonSetClient typedappsv1.DaemonSetInterface) (*DaemonSetCollectorUpdater, error) {
	collectorContainerName := os.Getenv(daemonSetCollectorContainerNameEnv)
	if collectorContainerName == "" {
		collectorContainerName = defaultDaemonSetCollectorContainerName
	}

	absCollectorUpdater := newAbstractCollectorUpdater(ctx)

	if err := absCollectorUpdater.setCollectorContainer(daemonSetCollector.Spec.Template.Spec.Containers, collectorContainerName); err != nil {
		return nil, fmt.Errorf("error setting daemonset collector container: %w", err)
	}

	if err := absCollectorUpdater.setCollectorResources(); err != nil {
		return nil, fmt.Errorf("error setting daemonset collector resources: %w", err)
	}

	resourcesConfigData, err := absCollectorUpdater.getResourcesConfigData()
	if err != nil {
		return nil, fmt.Errorf("error getting resources config data: %w", err)
	}

	daemonSetConditions := resourcesConfigData.DaemonSet

	return &DaemonSetCollectorUpdater{
		absCollectorUpdater: absCollectorUpdater,
		daemonSetCollector:  daemonSetCollector,
		daemonSetClient:     daemonSetClient,
		daemonSetConditions: &daemonSetConditions,
	}, nil
}

func NewStandaloneCollectorUpdater(ctx context.Context, standaloneCollector *appsv1.Deployment, standaloneClient typedappsv1.DeploymentInterface) (*StandaloneCollectorUpdater, error) {
	collectorContainerName := os.Getenv(standaloneCollectorContainerNameEnv)
	if collectorContainerName == "" {
		collectorContainerName = defaultStandaloneCollectorContainerName
	}

	absCollectorUpdater := newAbstractCollectorUpdater(ctx)

	if err := absCollectorUpdater.setCollectorContainer(standaloneCollector.Spec.Template.Spec.Containers, collectorContainerName); err != nil {
		return nil, fmt.Errorf("error setting standalone collector container: %w", err)
	}

	if err := absCollectorUpdater.setCollectorResources(); err != nil {
		return nil, fmt.Errorf("error setting standalone collector resources: %w", err)
	}

	resourcesConfigData, err := absCollectorUpdater.getResourcesConfigData()
	if err != nil {
		return nil, fmt.Errorf("error getting resources config data: %w", err)
	}

	standaloneConditions := resourcesConfigData.Standalone

	return &StandaloneCollectorUpdater{
		absCollectorUpdater:  absCollectorUpdater,
		standaloneCollector:  standaloneCollector,
		standaloneClient:     standaloneClient,
		standaloneConditions: &standaloneConditions,
	}, nil
}
