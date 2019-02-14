package metrics

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestMoleculerMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MoleculerMetrics Suite")
}
