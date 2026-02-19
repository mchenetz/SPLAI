package state

import (
	"context"
	"testing"
	"time"

	"github.com/example/daef/internal/observability"
)

func TestMemoryQueueClaimAckNackAndRequeueExpired(t *testing.T) {
	q := NewMemoryQueue()
	ctx := context.Background()

	_ = q.Enqueue(ctx, TaskRef{JobID: "j1", TaskID: "t1"})
	_ = q.Enqueue(ctx, TaskRef{JobID: "j1", TaskID: "t2"})

	claims, err := q.Claim(ctx, 2, "w1", 20*time.Millisecond)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("expected 2 claims, got %d", len(claims))
	}

	if err := q.Ack(ctx, []QueueClaim{claims[0]}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := q.Nack(ctx, []QueueClaim{claims[1]}, "not_ready"); err != nil {
		t.Fatalf("nack: %v", err)
	}

	claims2, err := q.Claim(ctx, 1, "w1", time.Second)
	if err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if len(claims2) != 1 || claims2[0].Ref.TaskID != "t2" {
		t.Fatalf("expected requeued t2 claim, got %+v", claims2)
	}

	if _, err := q.RequeueExpired(ctx, time.Now().UTC().Add(2*time.Second), 10); err != nil {
		t.Fatalf("requeue expired: %v", err)
	}
	claims3, err := q.Claim(ctx, 1, "w1", time.Second)
	if err != nil {
		t.Fatalf("claim3: %v", err)
	}
	if len(claims3) != 1 || claims3[0].Ref.TaskID != "t2" {
		t.Fatalf("expected expired claim to be requeued, got %+v", claims3)
	}
}

func TestMemoryQueueDeadLetterAndRequeue(t *testing.T) {
	observability.Default.Reset()
	q := NewMemoryQueue()
	ctx := context.Background()
	ref := TaskRef{JobID: "j2", TaskID: "t-dead"}
	_ = q.Enqueue(ctx, ref)

	for i := 0; i < 5; i++ {
		claims, err := q.Claim(ctx, 1, "w1", time.Second)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if len(claims) != 1 {
			t.Fatalf("expected claim on iteration %d", i)
		}
		if err := q.Nack(ctx, claims, "error"); err != nil {
			t.Fatalf("nack %d: %v", i, err)
		}
	}

	dead, err := q.ListDeadLetters(ctx, 10)
	if err != nil {
		t.Fatalf("list dead: %v", err)
	}
	if len(dead) != 1 || dead[0] != ref {
		t.Fatalf("expected one dead-letter task, got %+v", dead)
	}

	n, err := q.RequeueDeadLetters(ctx, []TaskRef{ref})
	if err != nil {
		t.Fatalf("requeue dead: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected requeued=1 got %d", n)
	}

	claims, err := q.Claim(ctx, 1, "w2", time.Second)
	if err != nil {
		t.Fatalf("claim after requeue: %v", err)
	}
	if len(claims) != 1 || claims[0].Ref != ref {
		t.Fatalf("expected task to reappear in pending, got %+v", claims)
	}
}

func TestMemoryQueueMetricsMove(t *testing.T) {
	observability.Default.Reset()
	q := NewMemoryQueue()
	ctx := context.Background()
	_ = q.Enqueue(ctx, TaskRef{JobID: "j3", TaskID: "t1"})

	claims, err := q.Claim(ctx, 1, "worker-a", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected one claim")
	}
	if err := q.Ack(ctx, claims); err != nil {
		t.Fatalf("ack: %v", err)
	}

	snap := observability.Default.Snapshot()
	if len(snap.Counters) == 0 {
		t.Fatalf("expected counters to be populated")
	}
	foundClaim := false
	foundAck := false
	for _, c := range snap.Counters {
		if c.Name == "queue_claimed_total" && c.Value >= 1 {
			foundClaim = true
		}
		if c.Name == "queue_acked_total" && c.Value >= 1 {
			foundAck = true
		}
	}
	if !foundClaim || !foundAck {
		t.Fatalf("expected claim and ack counters, got %+v", snap.Counters)
	}
}
