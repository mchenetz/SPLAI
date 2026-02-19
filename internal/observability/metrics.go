package observability

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type MetricPoint struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
}

type Snapshot struct {
	Counters []MetricPoint `json:"counters"`
	Gauges   []MetricPoint `json:"gauges"`
}

type metricEntry struct {
	name   string
	labels map[string]string
	value  float64
}

type Registry struct {
	mu       sync.Mutex
	counters map[string]metricEntry
	gauges   map[string]metricEntry
}

func NewRegistry() *Registry {
	return &Registry{
		counters: make(map[string]metricEntry),
		gauges:   make(map[string]metricEntry),
	}
}

var Default = NewRegistry()

func (r *Registry) IncCounter(name string, labels map[string]string, delta float64) {
	if delta == 0 {
		return
	}
	k, lcopy := metricKey(name, labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.counters[k]
	if e.name == "" {
		e = metricEntry{name: name, labels: lcopy}
	}
	e.value += delta
	r.counters[k] = e
}

func (r *Registry) SetGauge(name string, labels map[string]string, value float64) {
	k, lcopy := metricKey(name, labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[k] = metricEntry{name: name, labels: lcopy, value: value}
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Snapshot{
		Counters: make([]MetricPoint, 0, len(r.counters)),
		Gauges:   make([]MetricPoint, 0, len(r.gauges)),
	}
	for _, e := range r.counters {
		out.Counters = append(out.Counters, MetricPoint{Name: e.name, Labels: cloneMap(e.labels), Value: e.value})
	}
	for _, e := range r.gauges {
		out.Gauges = append(out.Gauges, MetricPoint{Name: e.name, Labels: cloneMap(e.labels), Value: e.value})
	}
	sort.Slice(out.Counters, func(i, j int) bool { return out.Counters[i].Name < out.Counters[j].Name })
	sort.Slice(out.Gauges, func(i, j int) bool { return out.Gauges[i].Name < out.Gauges[j].Name })
	return out
}

func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = make(map[string]metricEntry)
	r.gauges = make(map[string]metricEntry)
}

func (r *Registry) RenderPrometheus() string {
	s := r.Snapshot()
	lines := make([]string, 0, len(s.Counters)+len(s.Gauges))
	for _, p := range s.Counters {
		name := sanitizeMetricName(p.Name)
		lines = append(lines, formatPromLine(name, p.Labels, p.Value))
	}
	for _, p := range s.Gauges {
		name := sanitizeMetricName(p.Name)
		lines = append(lines, formatPromLine(name, p.Labels, p.Value))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func metricKey(name string, labels map[string]string) (string, map[string]string) {
	if len(labels) == 0 {
		return name, nil
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	parts = append(parts, name)
	copyLabels := make(map[string]string, len(labels))
	for _, k := range keys {
		v := labels[k]
		copyLabels[k] = v
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "|"), copyLabels
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sanitizeMetricName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "splai_metric"
	}
	out := make([]rune, 0, len(name))
	for i, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9' && i > 0)
		if valid {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

func formatPromLine(name string, labels map[string]string, value float64) string {
	if len(labels) == 0 {
		return name + " " + strconv.FormatFloat(value, 'f', -1, 64)
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", sanitizeMetricName(k), labels[k]))
	}
	return fmt.Sprintf("%s{%s} %s", name, strings.Join(parts, ","), strconv.FormatFloat(value, 'f', -1, 64))
}
