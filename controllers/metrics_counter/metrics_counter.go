package metrics_counter

import (
	"context"
	"fmt"
	"os"
	"strconv"
	
	"github.com/go-logr/logr"
	"github.com/logzio/kubernetes-monitoring-operator/controllers/collector"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	workersNumEnv = "WORKERS_NUM"

	defaultWorkersNum = 10
)

var (
	// logger for the entire package
	logger *logr.Logger = nil
)

type CollectorModeMetricsCounter interface {
	GetMetricsCount(collector.ReceiverConfigBuilder, collector.Collector) (int, error)
}

type DaemonSetCollectorMetricsCounter struct {
	absCollectorMetricsCounter *abstractCollectorMetricsCounter
	workersNum                 int
}

type StandaloneCollectorMetricsCounter struct {
	absCollectorMetricsCounter *abstractCollectorMetricsCounter
}

type abstractCollectorMetricsCounter struct {
	ctx       context.Context
	clientset *kubernetes.Clientset
}

func NewDaemonSetCollectorMetricCounter(ctx context.Context, clientset *kubernetes.Clientset) (*DaemonSetCollectorMetricsCounter, error) {
	absCollectorMetricsCounter, err := newAbstractCollectorMetricsCounter(ctx, clientset)
	if err != nil {
		return nil, fmt.Errorf("error creating abstract collector metrics counter: %w", err)
	}

	var workersNum int
	workersNumStr := os.Getenv(workersNumEnv)
	if workersNumStr == "" {
		workersNum = defaultWorkersNum
	} else {
		workersNumInt, err := strconv.Atoi(workersNumStr)
		if err != nil {
			workersNum = defaultWorkersNum
		} else {
			workersNum = workersNumInt
		}
	}

	return &DaemonSetCollectorMetricsCounter{
		absCollectorMetricsCounter: absCollectorMetricsCounter,
		workersNum:                 workersNum,
	}, nil
}

func NewStandaloneCollectorMetricCounter(ctx context.Context, clientset *kubernetes.Clientset) (*StandaloneCollectorMetricsCounter, error) {
	absCollectorMetricsCounter, err := newAbstractCollectorMetricsCounter(ctx, clientset)
	if err != nil {
		return nil, fmt.Errorf("error creating abstract collector metrics counter: %w", err)
	}

	return &StandaloneCollectorMetricsCounter{
		absCollectorMetricsCounter: absCollectorMetricsCounter,
	}, nil
}

func newAbstractCollectorMetricsCounter(ctx context.Context, clientset *kubernetes.Clientset) (*abstractCollectorMetricsCounter, error) {
	if logger == nil {
		newLogger := log.FromContext(ctx)
		logger = &newLogger
	}

	return &abstractCollectorMetricsCounter{
		ctx:       ctx,
		clientset: clientset,
	}, nil
}
