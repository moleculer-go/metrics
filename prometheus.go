package metrics

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/moleculer-go/moleculer"
	"github.com/moleculer-go/moleculer/payload"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

// PrometheusService Moleculer metrics module for Prometheus.
//  *
//  * 		https://prometheus.io/
//  *
//  * Running Prometheus & Grafana in Docker:
//  *
//  * 		git clone https://github.com/vegasbrianc/prometheus.git
//  * 		cd prometheus
//  *
//  * 	Please note, don't forget add your endpoint to static targets in prometheus/prometheus.yml file
//  *
//  *     static_configs:
//  *       - targets: ['localhost:9090', 'moleculer-node-123:3030']
//  *
//  *  Start containers:
//  *
//  * 		docker-compose up -d
//  *
//  * Grafana dashboard: http://<docker-ip>:3000
func PrometheusService() moleculer.ServiceSchema {

	collectors := make(map[string]prometheus.Collector)
	metricsCreatedChan := make(chan bool)
	metricsCreated := false
	// createMetrics create prometheus collectors for each of the metrics setup in the settings.
	createMetrics := func(settings map[string]interface{}, logger *log.Entry) {
		_, exists := settings["metrics"]
		if !exists {
			logger.Error("createMetrics() no metrics field found in the service settings!")
			return
		}

		metrics, ok := settings["metrics"].(map[string]interface{})
		if !ok {
			logger.Error("createMetrics()  metrics found in the service settings is of wrong type - it must be map[string]interface{}!")
			return
		}
		logger.Debug("prometheus.createMetrics() - metrics settings: ", metrics)

		for metricName, values := range metrics {
			params := values.(map[string]interface{})
			collector := collectorFromType(metricName, params, logger)
			collectors[metricName] = collector
			prometheus.MustRegister(collector)
		}
		metricsCreated = true
		metricsCreatedChan <- metricsCreated
	}

	traceSpanFinished := func(context moleculer.Context, payload moleculer.Payload) {
		service := payload.Get("service").Get("name").String()
		action := payload.Get("action").Get("name").String()
		nodeID := payload.Get("nodeID").String()
		duration := payload.Get("duration").Float()

		context.Logger().Debug("prometheus.traceSpanFinished() ... ")

		reqAllCount := collectors["moleculer_all_req_total"].(prometheus.Counter)
		reqAllCount.Inc()

		reqCount := collectors["moleculer_req_total"].(*prometheus.CounterVec)
		reqCount.With(prometheus.Labels{
			"action":  action,
			"service": service,
			"nodeID":  nodeID,
		}).Inc()

		reqAllDuration := collectors["moleculer_all_req_duration_ms"].(prometheus.Histogram)
		reqAllDuration.Observe(duration)

		reqDuration := collectors["moleculer_req_duration_ms"].(*prometheus.HistogramVec)
		reqDuration.With(prometheus.Labels{
			"action":  action,
			"service": service,
			"nodeID":  nodeID,
		}).Observe(duration)

		if payload.Get("error").Exists() {
			errorAllCount := collectors["moleculer_all_req_errors_total"].(prometheus.Counter)
			errorAllCount.Inc()

			errorCount := collectors["moleculer_req_errors_total"].(*prometheus.CounterVec)
			errorCount.With(prometheus.Labels{
				"action":       action,
				"service":      service,
				"nodeID":       nodeID,
				"errorMessage": payload.Get("error").Get("message").String(),
			}).Inc()
		}

	}

	updateMutex := sync.Mutex{}
	// updateCommonValues will fetch node.list, services, actions and events.
	// and update the collectors
	updateCommonValues := func(context moleculer.Context, params moleculer.Payload) {
		updateMutex.Lock()
		defer updateMutex.Unlock()
		context.Logger().Debug("prometheus.updateCommonValues() ... ")

		if !metricsCreated {
			context.Logger().Debug("prometheus.updateCommonValues() -> metrics not created... waiting for createMetrics() to finish!")
			<-metricsCreatedChan
			context.Logger().Debug("prometheus.updateCommonValues() -> createMetrics() finished --> updateCommonValues() will continue.")
		}
		results := <-context.MCall(map[string]map[string]interface{}{
			"nodes": map[string]interface{}{
				"action": "$node.list",
				"params": map[string]interface{}{
					"withServices":  false,
					"onlyAvailable": true,
				},
			},
			"services": map[string]interface{}{
				"action": "$node.services",
				"params": map[string]interface{}{
					"withActions":   false,
					"withEndpoints": true,
					"skipInternal":  true,
				},
			},
			"actions": map[string]interface{}{
				"action": "$node.actions",
				"params": map[string]interface{}{
					"withEndpoints": true,
					"skipInternal":  true,
					"onlyAvailable": true,
				},
			},
			"events": map[string]interface{}{
				"action": "$node.events",
				"params": map[string]interface{}{
					"withEndpoints": true,
					"skipInternal":  true,
					"onlyAvailable": true,
				},
			},
		})

		nodes := results["nodes"].MapArray()
		context.Logger().Debug("prometheus.updateCommonValues() -> nodes: ", nodes)

		nodesTotal := collectors["moleculer_nodes_total"].(prometheus.Gauge)
		nodesTotal.Set(float64(len(nodes)))

		nodesCollector := collectors["moleculer_nodes"].(*prometheus.GaugeVec)
		for _, item := range nodes {
			node := payload.New(item)
			var value float64
			if node.Get("available").Bool() {
				value = 1
			} else {
				value = 0
			}
			nodesCollector.With(prometheus.Labels{
				"nodeID":      node.Get("id").String(),
				"type":        node.Get("client").Get("type").String(),
				"version":     node.Get("client").Get("version").String(),
				"langVersion": node.Get("client").Get("langVersion").String(),
			}).Set(value)
		}

		services := results["services"].MapArray()
		context.Logger().Debug("prometheus.updateCommonValues() -> services: ", services)

		servicesTotal := collectors["moleculer_services_total"].(prometheus.Gauge)
		servicesTotal.Set(float64(len(services)))

		servicesCollector := collectors["moleculer_service_endpoints_total"].(*prometheus.GaugeVec)
		for _, service := range services {
			servicesCollector.With(prometheus.Labels{
				"service": service["name"].(string),
				"version": service["version"].(string),
			}).Set(float64(len(service["endpoints"].([]map[string]interface{}))))
		}

		actions := results["actions"].MapArray()
		context.Logger().Debug("prometheus.updateCommonValues() -> actions: ", actions)

		actionsTotal := collectors["moleculer_actions_total"].(prometheus.Gauge)
		actionsTotal.Set(float64(len(actions)))

		actionsCollector := collectors["moleculer_action_endpoints_total"].(*prometheus.GaugeVec)
		for _, action := range actions {
			actionsCollector.With(prometheus.Labels{
				"action": action["name"].(string),
			}).Set(float64(len(action["endpoints"].([]map[string]interface{}))))
		}

		events := results["events"].MapArray()
		context.Logger().Debug("prometheus.updateCommonValues() -> events: ", events)

		eventsTotal := collectors["moleculer_events_total"].(prometheus.Gauge)
		eventsTotal.Set(float64(len(events)))

		eventsCollector := collectors["moleculer_event_endpoints_total"].(*prometheus.GaugeVec)
		for _, event := range events {
			eventsCollector.With(prometheus.Labels{
				"event": event["name"].(string),
				"group": event["group"].(string),
			}).Set(float64(len(event["endpoints"].([]map[string]interface{}))))
		}

	}

	return moleculer.ServiceSchema{
		Name: "prometheus",
		Settings: map[string]interface{}{
			"port":                  3030,
			"endpoint":              "/metrics",
			"collectDefaultMetrics": true,
			"timeout":               10 * 1000,
			"metrics": map[string]interface{}{
				"moleculer_nodes_total": map[string]interface{}{
					"type": "Gauge",
					"help": "Moleculer nodes count",
				},
				"moleculer_services_total": map[string]interface{}{
					"type": "Gauge",
					"help": "Moleculer services count",
				},
				"moleculer_actions_total": map[string]interface{}{
					"type": "Gauge",
					"help": "Moleculer actions count",
				},
				"moleculer_events_total": map[string]interface{}{
					"type": "Gauge",
					"help": "Moleculer events subscriptions",
				},
				"moleculer_nodes": map[string]interface{}{
					"type":       "Gauge",
					"labelNames": []string{"nodeID", "type", "version", "langVersion"},
					"help":       "Moleculer node list",
				},
				"moleculer_action_endpoints_total": map[string]interface{}{
					"type":       "Gauge",
					"labelNames": []string{"action"},
					"help":       "Moleculer action endpoints",
				},
				"moleculer_service_endpoints_total": map[string]interface{}{
					"type":       "Gauge",
					"labelNames": []string{"service", "version"},
					"help":       "Moleculer service endpoints",
				},
				"moleculer_event_endpoints_total": map[string]interface{}{
					"type":       "Gauge",
					"labelNames": []string{"event", "group"},
					"help":       "Moleculer event endpoints",
				},
				"moleculer_all_req_total": map[string]interface{}{
					"type": "Counter",
					"help": "Moleculer all actions request count",
				},
				"moleculer_req_total": map[string]interface{}{
					"type":       "Counter",
					"labelNames": []string{"action", "service", "nodeID"},
					"help":       "Moleculer action request count",
				},
				"moleculer_all_req_errors_total": map[string]interface{}{
					"type": "Counter",
					"help": "Moleculer all request error count",
				},
				"moleculer_req_errors_total": map[string]interface{}{
					"type":       "Counter",
					"labelNames": []string{"action", "service", "nodeID", "errorMessage"},
					"help":       "Moleculer request error count",
				},
				"moleculer_all_req_duration_ms": map[string]interface{}{
					"type":    "Histogram",
					"help":    "Moleculer all request durations",
					"buckets": []float64{0.001, 0.01, 0.1, 0.5, 1.0, 10.0, 100.0, 300.0, 500.0, 700.0, 1000.0, 5000.0, 10000.0},
				},
				"moleculer_req_duration_ms": map[string]interface{}{
					"type":       "Histogram",
					"labelNames": []string{"action", "service", "nodeID"},
					"help":       "Moleculer request durations",
				},
			},
		},

		Events: []moleculer.Event{
			moleculer.Event{
				Name:    "$registry.service.added",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name:    "$registry.service.removed",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name:    "$node.connected",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name:    "$node.disconnected",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name:    "metrics.trace.span.finish",
				Handler: traceSpanFinished,
			},
		},
		Started: func(context moleculer.BrokerContext, service moleculer.ServiceSchema) {
			createMetrics(service.Settings, context.Logger())

			port := fmt.Sprint(":", service.Settings["port"])
			context.Logger().Debug("Prometheus collector service started! port: ", port)
			endpoint := service.Settings["endpoint"].(string)
			http.Handle(endpoint, promhttp.Handler())
			context.Logger().Fatal(http.ListenAndServe(port, nil))
		},
	}
}

// collectorFromType create a prometheus collector for the metric params.
func collectorFromType(metricName string, params map[string]interface{}, logger *log.Entry) prometheus.Collector {
	metricType := params["type"].(string)
	help := params["help"].(string)
	labelNames, hasLabels := params["labelNames"].([]string)
	buckets, hasBuckets := params["buckets"].([]float64)

	logger.Debug("Prometheus metrics -> creating metric: ", metricName, " type: ", metricType, " help: ", help, " labelNames: ", labelNames)
	switch metricType {
	case "Gauge":
		opts := prometheus.GaugeOpts{
			Name: metricName,
			Help: help,
		}
		if hasLabels {
			return prometheus.NewGaugeVec(opts, labelNames)
		}
		return prometheus.NewGauge(opts)

	case "Counter":
		opts := prometheus.CounterOpts{
			Name: metricName,
			Help: help,
		}
		if hasLabels {
			return prometheus.NewCounterVec(opts, labelNames)
		}
		return prometheus.NewCounter(opts)

	case "Histogram":
		opts := prometheus.HistogramOpts{
			Name: metricName,
			Help: help,
		}
		if hasBuckets {
			opts.Buckets = buckets
		}
		if hasLabels {
			return prometheus.NewHistogramVec(opts, labelNames)
		}
		return prometheus.NewHistogram(opts)
	}
	return nil
}
