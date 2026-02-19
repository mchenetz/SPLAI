package observability

import (
	"strings"
	"testing"
)

func TestRenderPrometheus(t *testing.T) {
	r := NewRegistry()
	r.IncCounter("queue_claimed_total", map[string]string{"queue_backend": "memory", "worker_id": "w1"}, 3)
	r.SetGauge("dead_letter_count", map[string]string{"queue_backend": "memory"}, 2)

	out := r.RenderPrometheus()
	if !strings.Contains(out, `queue_claimed_total{queue_backend="memory",worker_id="w1"} 3`) {
		t.Fatalf("missing claimed metric in output: %s", out)
	}
	if !strings.Contains(out, `dead_letter_count{queue_backend="memory"} 2`) {
		t.Fatalf("missing dead-letter gauge in output: %s", out)
	}
}
