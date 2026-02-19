package state

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRedisQueueIntegrationConcurrentClaims(t *testing.T) {
	addr := os.Getenv("DAEF_REDIS_ADDR_INTEGRATION")
	if addr == "" {
		t.Skip("set DAEF_REDIS_ADDR_INTEGRATION to run Redis integration tests")
	}
	ctx := context.Background()
	q := NewRedisQueue(RedisQueueConfig{Addr: addr, Key: "daef:test:integration:" + strconv.FormatInt(time.Now().UnixNano(), 10), Timeout: 2 * time.Second, DeadLetterMax: 3})

	// Seed tasks.
	for i := 0; i < 30; i++ {
		if err := q.Enqueue(ctx, TaskRef{JobID: "job-int", TaskID: "t-" + strconv.Itoa(i)}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	seen := sync.Map{}
	var wg sync.WaitGroup
	claimFn := func(worker string) {
		defer wg.Done()
		for {
			claims, err := q.Claim(ctx, 2, worker, 2*time.Second)
			if err != nil {
				t.Errorf("claim error: %v", err)
				return
			}
			if len(claims) == 0 {
				return
			}
			for _, c := range claims {
				k := c.Ref.JobID + "|" + c.Ref.TaskID
				if _, loaded := seen.LoadOrStore(k, true); loaded {
					t.Errorf("duplicate claim observed for %s", k)
				}
			}
			if err := q.Ack(ctx, claims); err != nil {
				t.Errorf("ack error: %v", err)
				return
			}
		}
	}

	wg.Add(2)
	go claimFn("w1")
	go claimFn("w2")
	wg.Wait()
}
