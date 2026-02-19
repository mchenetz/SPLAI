package state

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/example/daef/internal/observability"
)

type memoryInflight struct {
	claim QueueClaim
}

type MemoryQueue struct {
	mu       sync.Mutex
	items    []TaskRef
	inflight map[string]memoryInflight
	nack     map[string]int
	dead     []TaskRef
	counter  uint64
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		items:    make([]TaskRef, 0, 128),
		inflight: make(map[string]memoryInflight),
		nack:     make(map[string]int),
		dead:     make([]TaskRef, 0, 64),
	}
}

func (q *MemoryQueue) labels(extra map[string]string) map[string]string {
	l := map[string]string{"queue_backend": "memory"}
	for k, v := range extra {
		l[k] = v
	}
	return l
}

func (q *MemoryQueue) Enqueue(_ context.Context, ref TaskRef) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, ref)
	return nil
}

func (q *MemoryQueue) EnqueueMany(_ context.Context, refs []TaskRef) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, refs...)
	return nil
}

func (q *MemoryQueue) Claim(_ context.Context, max int, consumer string, visibilityTimeout time.Duration) ([]QueueClaim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if max <= 0 {
		max = 1
	}
	if visibilityTimeout <= 0 {
		visibilityTimeout = 15 * time.Second
	}
	if len(q.items) == 0 {
		return nil, nil
	}
	if max > len(q.items) {
		max = len(q.items)
	}
	now := time.Now().UTC()
	out := make([]QueueClaim, 0, max)
	for i := 0; i < max; i++ {
		ref := q.items[0]
		q.items = q.items[1:]
		q.counter++
		receipt := fmt.Sprintf("mem:%s:%d", consumer, q.counter)
		claim := QueueClaim{
			Ref:       ref,
			Receipt:   receipt,
			ClaimedBy: consumer,
			ClaimedAt: now,
			VisibleAt: now.Add(visibilityTimeout),
		}
		q.inflight[receipt] = memoryInflight{claim: claim}
		out = append(out, claim)
	}
	observability.Default.IncCounter("queue_claimed_total", q.labels(map[string]string{"worker_id": consumer}), float64(len(out)))
	return out, nil
}

func (q *MemoryQueue) Ack(_ context.Context, claims []QueueClaim) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, c := range claims {
		delete(q.inflight, c.Receipt)
		delete(q.nack, encodeTaskRef(c.Ref))
	}
	for _, c := range claims {
		observability.Default.IncCounter("queue_acked_total", q.labels(map[string]string{"worker_id": c.ClaimedBy}), 1)
	}
	return nil
}

func (q *MemoryQueue) Nack(_ context.Context, claims []QueueClaim, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, c := range claims {
		if inflight, ok := q.inflight[c.Receipt]; ok {
			ref := inflight.claim.Ref
			key := encodeTaskRef(ref)
			if reason == "error" {
				q.nack[key]++
				if q.nack[key] >= 5 {
					q.dead = append(q.dead, ref)
					delete(q.nack, key)
					delete(q.inflight, c.Receipt)
					continue
				}
			}
			q.items = append(q.items, ref)
			delete(q.inflight, c.Receipt)
		}
	}
	for _, c := range claims {
		observability.Default.IncCounter("queue_nacked_total", q.labels(map[string]string{"worker_id": c.ClaimedBy, "reason": reason}), 1)
	}
	observability.Default.SetGauge("dead_letter_count", q.labels(nil), float64(len(q.dead)))
	return nil
}

func (q *MemoryQueue) RequeueExpired(_ context.Context, now time.Time, max int) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	moved := 0
	for receipt, inflight := range q.inflight {
		if max > 0 && moved >= max {
			break
		}
		if inflight.claim.VisibleAt.After(now) {
			continue
		}
		q.items = append(q.items, inflight.claim.Ref)
		delete(q.inflight, receipt)
		moved++
	}
	if moved > 0 {
		observability.Default.IncCounter("queue_expired_requeued_total", q.labels(nil), float64(moved))
	}
	return moved, nil
}

func (q *MemoryQueue) ListDeadLetters(_ context.Context, limit int) ([]TaskRef, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 || limit > len(q.dead) {
		limit = len(q.dead)
	}
	out := make([]TaskRef, limit)
	copy(out, q.dead[:limit])
	return out, nil
}

func (q *MemoryQueue) RequeueDeadLetters(_ context.Context, refs []TaskRef) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(refs) == 0 {
		return 0, nil
	}
	target := make(map[string]int, len(refs))
	for _, r := range refs {
		target[encodeTaskRef(r)]++
	}
	kept := make([]TaskRef, 0, len(q.dead))
	requeued := 0
	for _, r := range q.dead {
		key := encodeTaskRef(r)
		if target[key] > 0 {
			q.items = append(q.items, r)
			target[key]--
			requeued++
			continue
		}
		kept = append(kept, r)
	}
	q.dead = kept
	if requeued > 0 {
		observability.Default.IncCounter("dead_letter_requeued_total", q.labels(nil), float64(requeued))
	}
	observability.Default.SetGauge("dead_letter_count", q.labels(nil), float64(len(q.dead)))
	return requeued, nil
}
