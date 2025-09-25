#!/bin/bash
# Fix all metrics references to metric

find . -name "*.go" -type f | while read file; do
    # Skip vendor and .git directories
    if [[ "$file" == *"vendor"* ]] || [[ "$file" == *".git"* ]]; then
        continue
    fi
    
    # Replace metrics. with metric. for all metric types
    sed -i 's/metrics\.Counter/metric.Counter/g' "$file"
    sed -i 's/metrics\.CounterVec/metric.CounterVec/g' "$file"
    sed -i 's/metrics\.CounterOpts/metric.CounterOpts/g' "$file"
    sed -i 's/metrics\.Gauge/metric.Gauge/g' "$file"
    sed -i 's/metrics\.GaugeVec/metric.GaugeVec/g' "$file"
    sed -i 's/metrics\.GaugeOpts/metric.GaugeOpts/g' "$file"
    sed -i 's/metrics\.Histogram/metric.Histogram/g' "$file"
    sed -i 's/metrics\.HistogramVec/metric.HistogramVec/g' "$file"
    sed -i 's/metrics\.HistogramOpts/metric.HistogramOpts/g' "$file"
    sed -i 's/metrics\.Summary/metric.Summary/g' "$file"
    sed -i 's/metrics\.SummaryVec/metric.SummaryVec/g' "$file"
    sed -i 's/metrics\.SummaryOpts/metric.SummaryOpts/g' "$file"
    sed -i 's/metrics\.Registerer/metric.Registerer/g' "$file"
    sed -i 's/metrics\.Registry/metric.Registry/g' "$file"
    sed -i 's/metrics\.Gatherer/metric.Gatherer/g' "$file"
    sed -i 's/metrics\.Collector/metric.Collector/g' "$file"
    sed -i 's/metrics\.NewCounter/metric.NewCounter/g' "$file"
    sed -i 's/metrics\.NewCounterVec/metric.NewCounterVec/g' "$file"
    sed -i 's/metrics\.NewGauge/metric.NewGauge/g' "$file"
    sed -i 's/metrics\.NewGaugeVec/metric.NewGaugeVec/g' "$file"
    sed -i 's/metrics\.NewHistogram/metric.NewHistogram/g' "$file"
    sed -i 's/metrics\.NewHistogramVec/metric.NewHistogramVec/g' "$file"
    sed -i 's/metrics\.NewSummary/metric.NewSummary/g' "$file"
    sed -i 's/metrics\.NewSummaryVec/metric.NewSummaryVec/g' "$file"
    sed -i 's/metrics\.Labels/metric.Labels/g' "$file"
    sed -i 's/metrics\.NewNoOpRegistry/metric.NewNoOpRegistry/g' "$file"
done
echo "Fixed all metrics references"
