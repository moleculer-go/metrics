package metrics

import (
	"net/http"
	"github.com/moleculer-go/moleculer"
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
func PrometheusService() *moleculer.Service {
	 
	var defaultCollector prometheus.Collector
	collectors := make(map[string]prometheus.Collector)
	
	// createMetrics create prometheus collectors for each of the metrics setup in the settings.
	func createMetrics(settings map[string]interface{}, logger *log.Entry) {

		if settings["collectDefaultMetrics"] != nil && settings["collectDefaultMetrics"].(bool) {
			defaultCollector = prometheus.NewGoCollector()		
		} 
		
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
		for metricName, values := range metrics {
			params := values.(map[string]interface{})
			collector := collectorFromType(metricName, params, logger)
			collectors[metricName] = collector
			prometheus.MustRegister(collector)
		}
	}

	func updateCommonValues(context moleculer.Context, payload moleculer.Payload) {
		
	}

	return &moleculer.Service{
		Name: "prometheus",
		Settings: map[string]interface{}{
			"port":                  3030,
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
				"moleculer_req_total": map[string]interface{}{
					"type":       "Counter",
					"labelNames": []string{"action", "service", "nodeID"},
					"help":       "Moleculer action request count",
				},
				"moleculer_req_errors_total": map[string]interface{}{
					"type":       "Counter",
					"labelNames": []string{"action", "service", "nodeID", "errorMessage"},
					"help":       "Moleculer request error count",
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
				Name: "$services.changed",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name: "$node.connected",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name: "$node.disconnected",
				Handler: updateCommonValues,
			},
			moleculer.Event{
				Name: "metrics.trace.span.finish",
				Handler: func(context moleculer.Context, payload moleculer.Payload) {
					service := payload.Get("service").Get("name").String()
					action := payload.Get("action").Get("name").String()
					nodeID := payload.Get("nodeID").String()
					duration := payload.Get("duration").Float()

					reqCount := collectors["moleculer_req_total"].(prometheus.CounterVec)
					reqCount.With(prometheus.Labels{
						"action":action,
						"service":service,
						"nodeID":nodeID,
					}).Inc()

					reqDuration := collectors["moleculer_req_duration_ms"].(prometheus.HistogramVec)
					reqDuration.With(prometheus.Labels{
						"action":action,
						"service":service,
						"nodeID":nodeID,
					}).Observe(duration)

					if payload.Get("error").Exists() {
						errorCount := collectors["moleculer_req_errors_total"].(prometheus.CounterVec)
						errorCount.With(prometheus.Labels{
							"action":action,
							"service":service,
							"nodeID":nodeID,
							"errorMessage": payload.Get("error").Get("message").String(),
						}).Inc()
					}
					
				}
			},
		},
		Started: func(service moleculer.Service, logger *log.Entry) {
			createMetrics(service.Settings, logger)

			port := service.Settings["port"].(int)
			http.Handle("/metrics", promhttp.Handler())
			logger.Fatal(http.ListenAndServe(string(port), nil))
		}
	}
}

func collectorFromType(metricName string, params map[string]interface{}, logger *log.Entry) prometheus.Collector {
	metricType := params["type"].(string)
	help:= params["help"].(string)
	labelNames, hasLabels:= params["labelNames"].([]string)

	logger.Debug("Prometheus metrics -> creating metric: ", metricName, " type: ", metricType, " help: ", help, " labelNames: ", labelNames)
	switch metricType {
	case "Gauge":
		if hasLabels {
			return prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: metricName,
				Help: help,
			}, labelNames)
		} 
		return prometheus.NewGauge(prometheus.GaugeOpts{
				Name: metricName,
				Help: help,
			})
		
	case "Counter":
		if hasLabels {
			return  prometheus.NewCounterVec(prometheus.GaugeOpts{
				Name: metricName,
				Help: help,
			}, labelNames)
		} 
		return  prometheus.NewCounter(prometheus.CounterOpts{
				Name: metricName,
				Help: help,
			}))
		
	case "Histogram":
		if hasLabels {
			return prometheus.NewHistogramVec(prometheus.GaugeOpts{
				Name: metricName,
				Help: help,
			}, labelNames)
		} 
		return prometheus.NewHistogram(prometheus.CounterOpts{
				Name: metricName,
				Help: help,
			}))
		
	}
	return nil
}

