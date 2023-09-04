package collector

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/yaml"
)

// BuildReceiverConfig builds prometheus receiver config
func (prcb *PrometheusReceiverConfigBuilder) BuildReceiverConfig(options ...ReceiverConfigBuilderOption) (string, error) {
	scrapeConfigsSize := 0
	receiverConfig := prometheusReceiverConfig{
		Global: map[string]string{
			"scrape_interval": fmt.Sprintf("%ss", prcb.receiverScrapeInterval),
			"scrape_timeout":  fmt.Sprintf("%ss", prcb.receiverScrapeInterval),
		},
	}

	configMapCollectorData, err := getConfigMapCollectorData(prcb.ctx, prcb.configMapClient, prcb.configMapCollectorNameSuffix)
	if err != nil {
		return "", fmt.Errorf("error getting the ConfigMap collector data: %w", err)
	}

	receivers, ok := configMapCollectorData["receivers"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("ConfigMap data does not have 'receivers' key")
	}

	cadvisorScraperConfigs, err := processReceiverScrapeConfigs("prometheus/cadvisor", receivers, options)
	if err != nil {
		return "", fmt.Errorf("error processing 'prometheus/cadvisor' receiver scrape configs: %w", err)
	}
	scrapeConfigsSize += len(cadvisorScraperConfigs)

	collectorScraperConfigs, err := processReceiverScrapeConfigs("prometheus/collector", receivers, options)
	if err != nil {
		return "", fmt.Errorf("error processing 'prometheus/collector' receiver scrape configs: %w", err)
	}
	scrapeConfigsSize += len(collectorScraperConfigs)

	infrastructureScraperConfigs, err := processReceiverScrapeConfigs("prometheus/infrastructure", receivers, options)
	if err != nil {
		return "", fmt.Errorf("error processing 'prometheus/infrastructure' receiver scrape configs: %w", err)
	}
	scrapeConfigsSize += len(infrastructureScraperConfigs)

	var applicationsScraperConfigs []interface{}
	isUsingApplicationMetrics, err := prcb.IsApplicationMetricsEnabled()
	if err != nil {
		return "", fmt.Errorf("error checking if application metrics is enabled: %w", err)
	}

	if isUsingApplicationMetrics {
		applicationsScraperConfigs, err = processReceiverScrapeConfigs("prometheus/applications", receivers, options)
		if err != nil {
			return "", fmt.Errorf("error processing 'prometheus/applications' receiver scrape configs: %w", err)
		}
		scrapeConfigsSize += len(infrastructureScraperConfigs)
	}

	receiverConfig.ScrapeConfigs = make([]interface{}, 0, scrapeConfigsSize)
	receiverConfig.ScrapeConfigs = append(receiverConfig.ScrapeConfigs, cadvisorScraperConfigs...)
	receiverConfig.ScrapeConfigs = append(receiverConfig.ScrapeConfigs, collectorScraperConfigs...)
	receiverConfig.ScrapeConfigs = append(receiverConfig.ScrapeConfigs, infrastructureScraperConfigs...)
	receiverConfig.ScrapeConfigs = append(receiverConfig.ScrapeConfigs, applicationsScraperConfigs...)

	newDataBytes, err := yaml.Marshal(&receiverConfig)
	if err != nil {
		return "", err
	}

	return string(newDataBytes), nil
}

// IsApplicationMetricsEnabled tells if application metrics is enabled by checking if '.service.pipelines' has 'metrics/applications'
func (prcb *PrometheusReceiverConfigBuilder) IsApplicationMetricsEnabled() (bool, error) {
	service, ok := prcb.configMapCollectorData["service"].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("ConfigMap data does not have 'service' key")
	}

	pipelines, ok := service["pipelines"].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("'service' does not have 'pipelines' key in the ConfigMap data")
	}

	if _, ok = pipelines["metrics/applications"].(map[string]interface{}); ok {
		return true, nil
	}

	return false, nil
}

// getConfigMapCollectorData gets the ConfigMap collector data
func getConfigMapCollectorData(ctx context.Context, configMapClient corev1.ConfigMapInterface, configMapCollectorNameSuffix string) (map[string]interface{}, error) {
	configMapList, err := configMapClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing ConfigMaps: %w", err)
	}

	isConfigMapExist := false
	data := make(map[string]interface{})

	for _, configMap := range configMapList.Items {
		if !strings.HasSuffix(configMap.Name, configMapCollectorNameSuffix) {
			continue
		}

		isConfigMapExist = true
		if err = yaml.Unmarshal([]byte(configMap.Data["relay"]), &data); err != nil {
			return nil, fmt.Errorf("error unmarshaling ConfigMap '%s' data", configMap.Name)
		}

		break
	}

	if !isConfigMapExist {
		return nil, fmt.Errorf("ConfigMap with the suffix '%s' could not be found", configMapCollectorNameSuffix)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ConfigMap data is empty")
	}

	return data, nil
}

// processReceiverScrapeConfigs processes a receiver's scrape configs from the ConfigMap collector receivers
func processReceiverScrapeConfigs(receiverName string, receivers map[string]interface{}, options []ReceiverConfigBuilderOption) ([]interface{}, error) {
	receiver, ok := receivers[receiverName].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("ConfigMap collector data does not have '%s' key under 'receivers' key", receiverName)
	}

	config, ok := receiver["config"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("ConfigMap collector data does not have 'config' key under 'receivers'")
	}

	scrapeConfigs, ok := config["scrape_configs"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("ConfigMap collector data does not have 'scrape_configs' key under 'config'")
	}

	for index, scrapeConfig := range scrapeConfigs {
		scrapeConfigMap, ok := scrapeConfig.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("scrape config is not a map")
		}

		_, ok = scrapeConfigMap["scrape_interval"]
		if ok {
			delete(scrapeConfigMap, "scrape_interval")
		}

		for _, option := range options {
			if err := option(scrapeConfigMap); err != nil {
				return nil, fmt.Errorf("error editing receiver '%s' scrape config #%d: %w", receiverName, index, err)
			}
		}
	}

	return scrapeConfigs, nil
}

// WithNode is an edit option for building the receiver config.
// Replaces 'kubernetes_sd_configs' node selector field '$KUBE_NODE_NAME' with nodeName
func WithNode(nodeName string) ReceiverConfigBuilderOption {
	return func(scrapeConfig map[string]interface{}) error {
		kubernetesSdConfigs, ok := scrapeConfig["kubernetes_sd_configs"].([]interface{})
		if !ok {
			// there are receivers with no 'kubernetes_sd_configs'
			return nil
		}

		for _, kubernetesSdConfig := range kubernetesSdConfigs {
			kubernetesSdConfigMap, ok := kubernetesSdConfig.(map[string]interface{})
			if !ok {
				return fmt.Errorf("option 'WithNode' error: kubernetes sd config in not a map")
			}

			selectors, ok := kubernetesSdConfigMap["selectors"].([]interface{})
			if !ok {
				return fmt.Errorf("option 'WithNode' error: scrape config does not have 'selectors' key under 'kubernetes_sd_configs'")
			}

			for _, selector := range selectors {
				selectorMap, ok := selector.(map[string]interface{})
				if !ok {
					return fmt.Errorf("option 'WithNode' error: selector in not a map")
				}

				field, ok := selectorMap["field"].(string)
				if !ok {
					return fmt.Errorf("option 'WithNode' error: selector does not have 'field' key")
				}

				selectorMap["field"] = strings.Replace(field, "$KUBE_NODE_NAME", nodeName, 1)
			}
		}

		return nil
	}
}

// WithKeepRelabelConfigsOnly is an edit option for building the receiver config.
// Lefts only keep actions in 'relabel_configs'.
func WithKeepRelabelConfigsOnly() ReceiverConfigBuilderOption {
	return func(scrapeConfig map[string]interface{}) error {
		relabelConfigs, ok := scrapeConfig["relabel_configs"].([]interface{})
		if !ok {
			// there are receivers with no 'relabel_configs'
			return nil
		}

		newRelabelConfigs := make([]interface{}, 0, 1)
		for _, relabelConfig := range relabelConfigs {
			relabelConfigMap, ok := relabelConfig.(map[string]interface{})
			if !ok {
				return fmt.Errorf("option 'WithKeepRelabelConfigsOnly' error: relabel config in not a map")
			}

			action, ok := relabelConfigMap["action"].(string)
			if ok && action != "keep" {
				continue
			}

			newRelabelConfigs = append(newRelabelConfigs, relabelConfig)
		}

		scrapeConfig["relabel_configs"] = newRelabelConfigs
		return nil
	}
}
