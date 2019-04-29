package metrics

import (
	"bufio"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/moleculer-go/moleculer"

	"github.com/moleculer-go/moleculer/broker"
	"github.com/moleculer-go/moleculer/transit/memory"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	log "github.com/sirupsen/logrus"
)

func getValue(text, name string) string {
	if strings.Contains(text, name) {
		scanner := bufio.NewScanner(strings.NewReader(text))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Index(line, "# HELP") == 0 || strings.Index(line, "# TYPE") == 0 {
				continue
			}
			if strings.Index(line, name) == 0 {
				return strings.TrimPrefix(line, name+" ")
			}
		}
	}
	return "not found"
}

func fetchResults() string {
	response, err := http.Get("http://localhost:3030/metrics")
	Expect(err).ShouldNot(HaveOccurred())
	defer response.Body.Close()
	bytes, err := ioutil.ReadAll(response.Body)
	Expect(err).ShouldNot(HaveOccurred())
	return string(bytes)
}

var _ = Describe("Prometheus", func() {
	slowActionHandler := func(ctx moleculer.Context, param moleculer.Payload) interface{} {
		if param.Get("sleepTime").Exists() {
			d := time.Duration(param.Get("sleepTime").Int64())
			time.Sleep(d * time.Nanosecond)
		}
		if param.Get("throwError").Exists() {
			//panic("I shuold throwError aaaaaahhhhhhh!")
			return errors.New("I shuold throwError aaaaaahhhhhhh!")
		}
		return nil
	}
	emptyEventHandler := func(ctx moleculer.Context, param moleculer.Payload) {}

	logLevel := "fatal"
	mem := &memory.SharedMemory{}
	var bkr, bkr2 *broker.ServiceBroker
	musicService := moleculer.ServiceSchema{
		Name: "music",
		Actions: []moleculer.Action{
			moleculer.Action{
				Name:    "start",
				Handler: slowActionHandler,
			},
			moleculer.Action{
				Name:    "end",
				Handler: slowActionHandler,
			},
		},
		Events: []moleculer.Event{
			moleculer.Event{
				Name:    "rings",
				Handler: emptyEventHandler,
			},
			moleculer.Event{
				Name:    "beeps",
				Handler: emptyEventHandler,
			},
		},
	}
	BeforeSuite(func() {
		bkr = broker.New(&moleculer.Config{
			Metrics:        true,
			LogLevel:       logLevel,
			DiscoverNodeID: func() string { return "Prometheus_Broker" },
			TransporterFactory: func() interface{} {
				transport := memory.Create(log.WithField("transport", "memory"), mem)
				return &transport
			},
		})
		bkr.Publish(PrometheusService())
		bkr.Start()
		time.Sleep(500 * time.Millisecond)
	})
	AfterSuite(func() {
		bkr.Stop()
	})

	It("Should have created metrics and started the webserver on default port 3030", func() {
		results := fetchResults()
		Expect(getValue(results, "moleculer_actions_total")).Should(Equal("0"))
		Expect(getValue(results, "moleculer_events_total")).Should(Equal("1"))
		Expect(getValue(results, "moleculer_nodes_total")).Should(Equal("1"))
		Expect(getValue(results, "moleculer_services_total")).Should(Equal("1"))
	})

	It("Should have updated the metrics after a new services was added", func() {
		bkr2 = broker.New(&moleculer.Config{
			Metrics:        true,
			LogLevel:       logLevel,
			DiscoverNodeID: func() string { return "Client_Broker_1" },
			TransporterFactory: func() interface{} {
				transport := memory.Create(log.WithField("transport", "memory"), mem)
				return &transport
			},
		})
		bkr2.Publish(musicService)
		bkr2.Start()
		time.Sleep(200 * time.Millisecond)

		results := fetchResults()
		Expect(getValue(results, "moleculer_actions_total")).Should(Equal("2"))
		Expect(getValue(results, "moleculer_events_total")).Should(Equal("3"))
		Expect(getValue(results, "moleculer_nodes_total")).Should(Equal("2"))
		Expect(getValue(results, "moleculer_services_total")).Should(Equal("2"))
	})

	It("Should have updated the metrics after slow actions are called", func() {
		bkr.Call("music.start", map[string]int{"sleepTime": 2})
		bkr.Call("music.end", map[string]int{"sleepTime": 1})
		bkr.Call("music.start", map[string]int{"sleepTime": 3})
		bkr.Call("music.end", map[string]int{"sleepTime": 4})
		bkr.Call("music.start", map[string]int{"sleepTime": 5})
		bkr.Call("music.end", map[string]int{"sleepTime": 6})

		bkr.Call("music.start", map[string]bool{"throwError": true})
		bkr.Call("music.end", map[string]bool{"throwError": true})

		time.Sleep(time.Millisecond * 1000)

		results := fetchResults()
		//fmt.Println("**************** \n ", results, "\n\n\n-")
		Expect(getValue(results, "moleculer_all_req_total")).Should(Equal("24"))
		Expect(getValue(results, "moleculer_all_req_duration_ms_count")).Should(Equal("24"))
		Expect(getValue(results, "moleculer_all_req_duration_ms_bucket{le=\"10\"}")).Should(Equal("24"))
		Expect(getValue(results, "moleculer_all_req_duration_ms_bucket{le=\"100\"}")).Should(Equal("24"))
		Expect(getValue(results, "moleculer_all_req_errors_total")).Should(Equal("2"))
	})

	It("Should have updated the metrics after a new services was removed", func() {
		bkr2.Stop()
		time.Sleep(200 * time.Millisecond)

		results := fetchResults()
		Expect(getValue(results, "moleculer_actions_total")).Should(Equal("0"))
		Expect(getValue(results, "moleculer_events_total")).Should(Equal("1"))
		Expect(getValue(results, "moleculer_nodes_total")).Should(Equal("1"))
		Expect(getValue(results, "moleculer_services_total")).Should(Equal("1"))
	})
})
