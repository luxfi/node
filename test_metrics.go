package main
import (
    "github.com/luxfi/metrics"
    "github.com/prometheus/client_golang/prometheus"
)
func main() {
    m := metrics.NewNoOpMetrics("test")
    reg := m.Registry()
    var _ prometheus.Registerer = reg
}
