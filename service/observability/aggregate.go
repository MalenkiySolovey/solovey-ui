package observability

func AggregateObservabilitySamples(samples []ObservabilitySample, dateTime int64) ObservabilitySample {
	if len(samples) == 0 {
		return ObservabilitySample{DateTime: dateTime}
	}
	var cpuTotal float64
	for _, sample := range samples {
		cpuTotal += sample.CPU
	}
	return ObservabilitySample{
		DateTime: dateTime,
		CPU:      cpuTotal / float64(len(samples)),
		Memory:   aggregateObservabilityMaps(samples, func(sample ObservabilitySample) map[string]interface{} { return sample.Memory }),
		Network:  aggregateObservabilityMaps(samples, func(sample ObservabilitySample) map[string]interface{} { return sample.Network }),
	}
}
func AggregateCoreSamples(samples []CoreSample, dateTime int64) CoreSample {
	if len(samples) == 0 {
		return CoreSample{DateTime: dateTime}
	}
	latest := samples[len(samples)-1]
	latest.DateTime = dateTime
	return latest
}
func (sample ObservabilitySample) metricValue(metric ObservabilityMetric) (float64, bool) {
	switch metric {
	case ObservabilityMetricCPU:
		return sample.CPU, true
	case ObservabilityMetricRAM:
		return mapNumericValue(sample.Memory, "current")
	case ObservabilityMetricNetIn:
		return mapNumericValue(sample.Network, "recv")
	case ObservabilityMetricNetOut:
		return mapNumericValue(sample.Network, "sent")
	default:
		return 0, false
	}
}
func mapNumericValue(values map[string]interface{}, key string) (float64, bool) {
	if values == nil {
		return 0, false
	}
	return observabilityNumericValue(values[key])
}
func aggregateObservabilityMaps(samples []ObservabilitySample, selector func(ObservabilitySample) map[string]interface{}) map[string]interface{} {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, sample := range samples {
		for key, value := range selector(sample) {
			numeric, ok := observabilityNumericValue(value)
			if !ok {
				continue
			}
			sums[key] += numeric
			counts[key]++
		}
	}
	aggregated := make(map[string]interface{}, len(sums))
	for key, sum := range sums {
		aggregated[key] = sum / float64(counts[key])
	}
	return aggregated
}
func observabilityNumericValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}
