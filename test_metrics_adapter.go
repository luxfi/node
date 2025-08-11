package main
import (
    "fmt"
    "github.com/luxfi/metrics"
)
func main() {
    m := metrics.NewNoOpMetrics("test")
    reg := m.Registry()
    promReg, ok := metrics.UnwrapPrometheusRegistry(reg)
    if ok && promReg \!= nil {
        fmt.Println("Got prometheus registry")
    } else {
        fmt.Println("No prometheus registry available")
    }
}
