/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	typedappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1 "github.com/logzio/kubernetes-monitoring-operator/api/v1"
	"github.com/logzio/kubernetes-monitoring-operator/controllers/collector"
	metricsCounter "github.com/logzio/kubernetes-monitoring-operator/controllers/metrics_counter"
	"github.com/logzio/kubernetes-monitoring-operator/controllers/updater"
)

const (
	namespaceEnv                              = "NAMESPACE"
	timeIntervalEnv                           = "TIME_INTERVAL"
	memcachedNameEnv                          = "MEMCACHED_NAME"
	daemonSetCollectorNameSuffixEnv           = "DAEMONSET_COLLECTOR_NAME_SUFFIX"
	standaloneCollectorNameSuffixEnv          = "STANDALONE_COLLECTOR_NAME_SUFFIX"
	configMapDaemonSetCollectorNameSuffixEnv  = "CONFIGMAP_DAEMONSET_COLLECTOR_SUFFIX"
	configMapStandaloneCollectorNameSuffixEnv = "CONFIGMAP_STANDALONE_COLLECTOR_SUFFIX"

	defaultNamespace                              = "monitoring"
	defaultTimeInterval                           = 5
	defaultMemcachedName                          = "monitoring-operator-memcached"
	defaultDaemonSetCollectorNameSuffix           = "otel-collector-ds"
	defaultStandaloneCollectorNameSuffix          = "otel-collector-standalone"
	defaultConfigMapDaemonSetCollectorNameSuffix  = "otel-collector-ds"
	defaultConfigMapStandaloneCollectorNameSuffix = "otel-collector-standalone"
)

// CollectorResourcesReconciler reconciles the Logz.io monitoring collector limits
type CollectorResourcesReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	ctx       context.Context
	logger    logr.Logger
	settings  *reconcilerSettings
	clientset *kubernetes.Clientset
}

// reconcilerSettings is the settings of the reconciler
type reconcilerSettings struct {
	namespace                     string
	timeInterval                  int
	memcachedName                 string
	workersNum                    int
	daemonSetCollectorNameSuffix  string
	standaloneCollectorNameSuffix string
}

type collectorModeManager struct {
	receiverConfigBuilder collector.ReceiverConfigBuilder
	collector             collector.Collector
	metricsCounter        metricsCounter.CollectorModeMetricsCounter
	updater               updater.CollectorModeUpdater
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *CollectorResourcesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Create logger
	r.logger = log.FromContext(ctx)

	r.ctx = ctx
	clientset, err := createClientset()
	if err != nil {
		r.logger.Error(err, "Failed to create a Clientset")
		return ctrl.Result{}, err
	}

	r.clientset = clientset

	if err = r.newReconcilerSettings(); err != nil {
		return ctrl.Result{}, err
	}

	// Get Memcached if exists, otherwise create new one
	memcached, err := r.getMemcached()
	if err != nil {
		r.logger.Error(err, "Failed to get Memcached")
		return ctrl.Result{}, err
	}
	if memcached == nil {
		return ctrl.Result{}, nil
	}

	// First run of Reconcile
	if memcached.Spec.LastTimestamp == "" {
		if err = r.reconcileCollectorResources(ctx, memcached); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	lastTimestamp, err := time.Parse(time.UnixDate, memcached.Spec.LastTimestamp)
	if err != nil {
		r.logger.Error(err, "Failed to parse Memcached LastTimestamp")
		return ctrl.Result{}, err
	}

	if time.Now().Before(lastTimestamp.Add(time.Minute * time.Duration(r.settings.timeInterval))) {
		r.logger.Info("Time interval has not yet passed", "timeInterval", fmt.Sprintf("%dm", r.settings.timeInterval))
		return ctrl.Result{}, nil
	}

	if err = r.reconcileCollectorResources(ctx, memcached); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CollectorResourcesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

func (r *CollectorResourcesReconciler) newReconcilerSettings() error {
	namespace := os.Getenv(namespaceEnv)
	if namespace == "" {
		namespace = defaultNamespace
	}

	var timeInterval int
	timeIntervalStr := os.Getenv(timeIntervalEnv)
	if timeIntervalStr == "" {
		timeInterval = defaultTimeInterval
	} else {
		timeIntervalInt, err := strconv.Atoi(timeIntervalStr)
		if err != nil {
			timeInterval = defaultTimeInterval
		} else {
			timeInterval = timeIntervalInt
		}
	}

	memcachedName := os.Getenv(memcachedNameEnv)
	if memcachedName == "" {
		memcachedName = defaultMemcachedName
	}

	daemonSetCollectorNameSuffix := os.Getenv(daemonSetCollectorNameSuffixEnv)
	if daemonSetCollectorNameSuffix == "" {
		daemonSetCollectorNameSuffix = defaultDaemonSetCollectorNameSuffix
	}

	standaloneCollectorNameSuffix := os.Getenv(standaloneCollectorNameSuffixEnv)
	if standaloneCollectorNameSuffix == "" {
		standaloneCollectorNameSuffix = defaultStandaloneCollectorNameSuffix
	}

	r.settings = &reconcilerSettings{
		namespace:                     namespace,
		timeInterval:                  timeInterval,
		memcachedName:                 memcachedName,
		daemonSetCollectorNameSuffix:  daemonSetCollectorNameSuffix,
		standaloneCollectorNameSuffix: standaloneCollectorNameSuffix,
	}

	return nil
}

// createClientset creates the clientset (kubernetes client)
func createClientset() (*kubernetes.Clientset, error) {
	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating a Clientset using in-cluster config")
	}

	return clientset, nil
}

// getMemcached returns the Memcached custom resource
func (r *CollectorResourcesReconciler) getMemcached() (*cachev1.Memcached, error) {
	memcached := &cachev1.Memcached{}
	namespacedName := types.NamespacedName{
		Name:      r.settings.memcachedName,
		Namespace: r.settings.namespace,
	}

	// Get Memcached if exists
	if err := r.Get(r.ctx, namespacedName, memcached); err != nil {
		r.logger.Info("Memcached does not exist. Creating new Memcached")
		memcached = &cachev1.Memcached{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.settings.memcachedName,
				Namespace: r.settings.namespace,
			},
			Spec: cachev1.MemcachedSpec{},
		}

		// Create Memcached
		if err = r.Create(r.ctx, memcached); err != nil {
			// Catch more than one call is trying to create Memcached
			if apierrors.IsAlreadyExists(err) {
				r.logger.Info("Memcached is already exists", "name", r.settings.memcachedName, "namespace", r.settings.namespace)
				return nil, nil
			}

			return nil, fmt.Errorf("error creating new Memcached '%s' in namespace '%s': %w", r.settings.memcachedName, r.settings.namespace, err)
		}
	}

	return memcached, nil
}

func (r *CollectorResourcesReconciler) reconcileCollectorResources(ctx context.Context, memcached *cachev1.Memcached) error {
	// Update Memcached
	if err := r.updateMemcached(memcached); err != nil {
		return err
	}

	collectorManager, err := r.buildCollectorModeManager(ctx)
	if err != nil {
		return fmt.Errorf("error building collector mode manager: %w", err)
	}

	metricsCount, err := collectorManager.metricsCounter.GetMetricsCount(collectorManager.receiverConfigBuilder, collectorManager.collector)
	if err != nil {
		return fmt.Errorf("error getting metrics count: %w", err)
	}

	isUsingApplicationMetrics, err := collectorManager.receiverConfigBuilder.IsApplicationMetricsEnabled()
	if err != nil {
		return fmt.Errorf("error getting is application metrics enabled: %w", err)
	}

	if err = collectorManager.updater.UpdateResources(metricsCount, isUsingApplicationMetrics); err != nil {
		return fmt.Errorf("error updating resources: %w", err)
	}

	return nil
}

func (r *CollectorResourcesReconciler) updateMemcached(memcached *cachev1.Memcached) error {
	memcached.Spec.LastTimestamp = time.Now().Format(time.UnixDate)
	r.logger.Info("Updating Memcached", "lastTimestamp", memcached.Spec.LastTimestamp)

	// Update Memcached
	if err := r.Update(r.ctx, memcached); err != nil {
		return fmt.Errorf("error updating Memcached: %w", err)
	}

	return nil
}

func (r *CollectorResourcesReconciler) getDaemonSetCollector(daemonSetClient typedappsv1.DaemonSetInterface) (*appsv1.DaemonSet, error) {
	daemonSetList, err := daemonSetClient.List(r.ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing daemonsets: %w", err)
	}

	for _, daemonSet := range daemonSetList.Items {
		if strings.HasSuffix(daemonSet.Name, r.settings.daemonSetCollectorNameSuffix) {
			return &daemonSet, nil
		}
	}

	return nil, nil
}

func (r *CollectorResourcesReconciler) getStandaloneCollector(standaloneClient typedappsv1.DeploymentInterface) (*appsv1.Deployment, error) {
	deploymentList, err := standaloneClient.List(r.ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing deployments: %w", err)
	}

	for _, deployment := range deploymentList.Items {
		if strings.HasSuffix(deployment.Name, r.settings.standaloneCollectorNameSuffix) {
			return &deployment, err
		}
	}

	return nil, nil
}

func (r *CollectorResourcesReconciler) buildCollectorModeManager(ctx context.Context) (*collectorModeManager, error) {
	var daemonSetCollector *appsv1.DaemonSet
	var standaloneCollector *appsv1.Deployment

	daemonSetClient := r.clientset.AppsV1().DaemonSets(r.settings.namespace)
	standaloneClient := r.clientset.AppsV1().Deployments(r.settings.namespace)

	daemonSetCollector, err := r.getDaemonSetCollector(daemonSetClient)
	if err != nil {
		r.logger.Error(err, "Failed to get daemonset collector")
		return nil, err
	}
	if daemonSetCollector == nil {
		standaloneCollector, err = r.getStandaloneCollector(standaloneClient)
		if err != nil {
			r.logger.Error(err, "Failed to get standalone collector")
			return nil, err
		}
	}

	if daemonSetCollector == nil && standaloneCollector == nil {
		r.logger.Error(err, "daemonset collector with suffix '%s' and standalone collector with suffix '%s' were not found", r.settings.daemonSetCollectorNameSuffix, r.settings.standaloneCollectorNameSuffix)
		return nil, err
	}

	var receiverConfigBuilder collector.ReceiverConfigBuilder
	configMapClient := r.clientset.CoreV1().ConfigMaps(r.settings.namespace)

	var collec collector.Collector
	collec, err = collector.NewPrometheusCollector(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus collector")
	}

	var collectorModeMetricsCounter metricsCounter.CollectorModeMetricsCounter
	var collectorModeUpdater updater.CollectorModeUpdater

	if daemonSetCollector != nil {
		configMapCollectorNameSuffix := os.Getenv(configMapDaemonSetCollectorNameSuffixEnv)
		if configMapCollectorNameSuffix == "" {
			configMapCollectorNameSuffix = defaultConfigMapDaemonSetCollectorNameSuffix
		}

		receiverConfigBuilder, err = collector.NewPrometheusReceiverConfigBuilder(ctx, configMapClient, configMapCollectorNameSuffix)
		if err != nil {
			return nil, fmt.Errorf("error creating new prometheus receiver config builder: %w", err)
		}

		collectorModeMetricsCounter, err = metricsCounter.NewDaemonSetCollectorMetricCounter(ctx, r.clientset)
		collectorModeUpdater, err = updater.NewDaemonSetCollectorUpdater(ctx, daemonSetCollector, daemonSetClient)

	} else if standaloneCollector != nil {
		configMapCollectorNameSuffix := os.Getenv(configMapStandaloneCollectorNameSuffixEnv)
		if configMapCollectorNameSuffix == "" {
			configMapCollectorNameSuffix = defaultConfigMapStandaloneCollectorNameSuffix
		}

		receiverConfigBuilder, err = collector.NewPrometheusReceiverConfigBuilder(ctx, configMapClient, configMapCollectorNameSuffix)
		if err != nil {
			return nil, fmt.Errorf("error creating new prometheus receiver config builder: %w", err)
		}

		collectorModeMetricsCounter, err = metricsCounter.NewStandaloneCollectorMetricCounter(ctx, r.clientset)
		collectorModeUpdater, err = updater.NewStandaloneCollectorUpdater(ctx, standaloneCollector, standaloneClient)
	}

	return &collectorModeManager{
		receiverConfigBuilder: receiverConfigBuilder,
		collector:             collec,
		metricsCounter:        collectorModeMetricsCounter,
		updater:               collectorModeUpdater,
	}, nil
}
