package metrics

import (
	"bufio"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/moleculer-go/moleculer"

	"github.com/moleculer-go/moleculer/broker"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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

var _ = Describe("Prometheus", func() {
	emptyActionHandler := func(ctx moleculer.Context, param moleculer.Payload) interface{} { return nil }
	emptyEventHandler := func(ctx moleculer.Context, param moleculer.Payload) {}

	var bkr *broker.ServiceBroker
	musicService := moleculer.Service{
		Name: "music",
		Actions: []moleculer.Action{
			moleculer.Action{
				Name:    "start",
				Handler: emptyActionHandler,
			},
			moleculer.Action{
				Name:    "end",
				Handler: emptyActionHandler,
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
		bkr = broker.FromConfig(&moleculer.BrokerConfig{
			LogLevel: "debug",
		})
		bkr.AddService(PrometheusService())
		bkr.Start()
	})
	AfterSuite(func() {
		bkr.Stop()
	})

	It("Should have created metrics and started the webserver on default port 3030", func() {
		response, err := http.Get("http://localhost:3030/metrics")
		Expect(err).ShouldNot(HaveOccurred())
		defer response.Body.Close()
		bytes, err := ioutil.ReadAll(response.Body)
		Expect(err).ShouldNot(HaveOccurred())
		results := string(bytes)
		Expect(getValue(results, "moleculer_actions_total")).Should(Equal("0"))
		Expect(getValue(results, "moleculer_events_total")).Should(Equal("0"))
		Expect(getValue(results, "moleculer_nodes_total")).Should(Equal("0"))
		Expect(getValue(results, "moleculer_services_total")).Should(Equal("0"))
	})

	It("Should have updated the metrics after a new services was added", func() {

		bkr.AddService(musicService)

		response, err := http.Get("http://localhost:3030/metrics")
		Expect(err).ShouldNot(HaveOccurred())
		defer response.Body.Close()
		bytes, err := ioutil.ReadAll(response.Body)
		Expect(err).ShouldNot(HaveOccurred())
		results := string(bytes)
		Expect(getValue(results, "moleculer_actions_total")).Should(Equal("2"))
		Expect(getValue(results, "moleculer_events_total")).Should(Equal("2"))
		Expect(getValue(results, "moleculer_nodes_total")).Should(Equal("0"))
		Expect(getValue(results, "moleculer_services_total")).Should(Equal("1"))
	})
})
