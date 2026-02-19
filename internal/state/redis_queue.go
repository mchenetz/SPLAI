package state

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/example/daef/internal/observability"
)

type RedisQueueConfig struct {
	Addr          string
	Password      string
	DB            int
	Key           string
	Timeout       time.Duration
	DeadLetterMax int
}

type RedisQueue struct {
	cfg RedisQueueConfig
}

func NewRedisQueue(cfg RedisQueueConfig) *RedisQueue {
	if cfg.Key == "" {
		cfg.Key = "daef:tasks"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.DeadLetterMax <= 0 {
		cfg.DeadLetterMax = 5
	}
	return &RedisQueue{cfg: cfg}
}

func (q *RedisQueue) pendingKey() string    { return q.cfg.Key + ":pending" }
func (q *RedisQueue) claimsKey() string     { return q.cfg.Key + ":claims" }
func (q *RedisQueue) visibilityKey() string { return q.cfg.Key + ":visibility" }
func (q *RedisQueue) nackKey() string       { return q.cfg.Key + ":nack" }
func (q *RedisQueue) deadKey() string       { return q.cfg.Key + ":dead" }

func (q *RedisQueue) labels(extra map[string]string) map[string]string {
	l := map[string]string{"queue_backend": "redis"}
	for k, v := range extra {
		l[k] = v
	}
	return l
}

func (q *RedisQueue) Enqueue(ctx context.Context, ref TaskRef) error {
	return q.EnqueueMany(ctx, []TaskRef{ref})
}

func (q *RedisQueue) EnqueueMany(ctx context.Context, refs []TaskRef) error {
	if len(refs) == 0 {
		return nil
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	args := make([]string, 0, len(refs)+1)
	args = append(args, q.pendingKey())
	for _, ref := range refs {
		args = append(args, encodeTaskRef(ref))
	}
	if err := writeRESP(rw, append([]string{"LPUSH"}, args...)...); err != nil {
		return err
	}
	_, err = readRESP(rw)
	return err
}

func (q *RedisQueue) Claim(ctx context.Context, max int, consumer string, visibilityTimeout time.Duration) ([]QueueClaim, error) {
	if max <= 0 {
		max = 1
	}
	if visibilityTimeout <= 0 {
		visibilityTimeout = 15 * time.Second
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	now := time.Now().UTC()
	out := make([]QueueClaim, 0, max)
	for i := 0; i < max; i++ {
		if err := writeRESP(rw, "RPOP", q.pendingKey()); err != nil {
			return nil, err
		}
		resp, err := readRESP(rw)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			break
		}
		raw, ok := resp.(string)
		if !ok {
			return nil, errors.New("unexpected redis response type")
		}
		ref, ok := decodeTaskRef(raw)
		if !ok {
			_ = q.pushDeadRaw(ctx, raw)
			continue
		}

		receipt := fmt.Sprintf("%s:%d:%d", consumer, time.Now().UnixNano(), i)
		visibleAt := now.Add(visibilityTimeout)
		if err := writeRESP(rw, "HSET", q.claimsKey(), receipt, raw); err != nil {
			return nil, err
		}
		if _, err := readRESP(rw); err != nil {
			return nil, err
		}
		if err := writeRESP(rw, "ZADD", q.visibilityKey(), strconv.FormatInt(visibleAt.UnixMilli(), 10), receipt); err != nil {
			return nil, err
		}
		if _, err := readRESP(rw); err != nil {
			return nil, err
		}

		out = append(out, QueueClaim{
			Ref:       ref,
			Receipt:   receipt,
			ClaimedBy: consumer,
			ClaimedAt: now,
			VisibleAt: visibleAt,
		})
	}
	observability.Default.IncCounter("queue_claimed_total", q.labels(map[string]string{"worker_id": consumer}), float64(len(out)))
	return out, nil
}

func (q *RedisQueue) Ack(ctx context.Context, claims []QueueClaim) error {
	if len(claims) == 0 {
		return nil
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, c := range claims {
		payload, err := q.getClaimPayload(rw, c.Receipt)
		if err != nil {
			return err
		}
		if err := writeRESP(rw, "HDEL", q.claimsKey(), c.Receipt); err != nil {
			return err
		}
		if _, err := readRESP(rw); err != nil {
			return err
		}
		if err := writeRESP(rw, "ZREM", q.visibilityKey(), c.Receipt); err != nil {
			return err
		}
		if _, err := readRESP(rw); err != nil {
			return err
		}
		if payload != "" {
			if err := writeRESP(rw, "HDEL", q.nackKey(), payload); err != nil {
				return err
			}
			if _, err := readRESP(rw); err != nil {
				return err
			}
		}
	}
	for _, c := range claims {
		observability.Default.IncCounter("queue_acked_total", q.labels(map[string]string{"worker_id": c.ClaimedBy}), 1)
	}
	return nil
}

func (q *RedisQueue) Nack(ctx context.Context, claims []QueueClaim, reason string) error {
	if len(claims) == 0 {
		return nil
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, c := range claims {
		payload, err := q.getClaimPayload(rw, c.Receipt)
		if err != nil {
			return err
		}
		if payload == "" {
			continue
		}

		toDead := false
		if reason == "error" {
			count, err := q.incrNack(rw, payload)
			if err != nil {
				return err
			}
			toDead = count >= q.cfg.DeadLetterMax
		}

		if toDead {
			if err := writeRESP(rw, "LPUSH", q.deadKey(), payload); err != nil {
				return err
			}
			if _, err := readRESP(rw); err != nil {
				return err
			}
			if err := writeRESP(rw, "HDEL", q.nackKey(), payload); err != nil {
				return err
			}
			if _, err := readRESP(rw); err != nil {
				return err
			}
		} else {
			if err := writeRESP(rw, "LPUSH", q.pendingKey(), payload); err != nil {
				return err
			}
			if _, err := readRESP(rw); err != nil {
				return err
			}
		}

		if err := writeRESP(rw, "HDEL", q.claimsKey(), c.Receipt); err != nil {
			return err
		}
		if _, err := readRESP(rw); err != nil {
			return err
		}
		if err := writeRESP(rw, "ZREM", q.visibilityKey(), c.Receipt); err != nil {
			return err
		}
		if _, err := readRESP(rw); err != nil {
			return err
		}
	}
	for _, c := range claims {
		observability.Default.IncCounter("queue_nacked_total", q.labels(map[string]string{"worker_id": c.ClaimedBy, "reason": reason}), 1)
	}
	if err := q.refreshDeadGauge(ctx); err != nil {
		return err
	}
	return nil
}

func (q *RedisQueue) RequeueExpired(ctx context.Context, now time.Time, max int) (int, error) {
	if max <= 0 {
		max = 100
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if err := writeRESP(rw, "ZRANGEBYSCORE", q.visibilityKey(), "-inf", strconv.FormatInt(now.UnixMilli(), 10), "LIMIT", "0", strconv.Itoa(max)); err != nil {
		return 0, err
	}
	resp, err := readRESP(rw)
	if err != nil {
		return 0, err
	}
	receipts, err := toStringArray(resp)
	if err != nil {
		return 0, err
	}
	for _, receipt := range receipts {
		payload, err := q.getClaimPayload(rw, receipt)
		if err != nil {
			return 0, err
		}
		if payload != "" {
			if err := writeRESP(rw, "LPUSH", q.pendingKey(), payload); err != nil {
				return 0, err
			}
			if _, err := readRESP(rw); err != nil {
				return 0, err
			}
		}
		if err := writeRESP(rw, "HDEL", q.claimsKey(), receipt); err != nil {
			return 0, err
		}
		if _, err := readRESP(rw); err != nil {
			return 0, err
		}
		if err := writeRESP(rw, "ZREM", q.visibilityKey(), receipt); err != nil {
			return 0, err
		}
		if _, err := readRESP(rw); err != nil {
			return 0, err
		}
	}
	if len(receipts) > 0 {
		observability.Default.IncCounter("queue_expired_requeued_total", q.labels(nil), float64(len(receipts)))
	}
	return len(receipts), nil
}

func (q *RedisQueue) ListDeadLetters(ctx context.Context, limit int) ([]TaskRef, error) {
	if limit <= 0 {
		limit = 50
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := writeRESP(rw, "LRANGE", q.deadKey(), "0", strconv.Itoa(limit-1)); err != nil {
		return nil, err
	}
	resp, err := readRESP(rw)
	if err != nil {
		return nil, err
	}
	items, err := toStringArray(resp)
	if err != nil {
		return nil, err
	}
	out := make([]TaskRef, 0, len(items))
	for _, raw := range items {
		ref, ok := decodeTaskRef(raw)
		if !ok {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}

func (q *RedisQueue) RequeueDeadLetters(ctx context.Context, refs []TaskRef) (int, error) {
	if len(refs) == 0 {
		return 0, nil
	}
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	requeued := 0
	for _, ref := range refs {
		raw := encodeTaskRef(ref)
		if err := writeRESP(rw, "LREM", q.deadKey(), "1", raw); err != nil {
			return requeued, err
		}
		resp, err := readRESP(rw)
		if err != nil {
			return requeued, err
		}
		removed, err := atoiRESP(resp)
		if err != nil {
			return requeued, err
		}
		if removed == 0 {
			continue
		}
		if err := writeRESP(rw, "LPUSH", q.pendingKey(), raw); err != nil {
			return requeued, err
		}
		if _, err := readRESP(rw); err != nil {
			return requeued, err
		}
		if err := writeRESP(rw, "HDEL", q.nackKey(), raw); err != nil {
			return requeued, err
		}
		if _, err := readRESP(rw); err != nil {
			return requeued, err
		}
		requeued++
	}
	if requeued > 0 {
		observability.Default.IncCounter("dead_letter_requeued_total", q.labels(nil), float64(requeued))
	}
	if err := q.refreshDeadGauge(ctx); err != nil {
		return requeued, err
	}
	return requeued, nil
}

func (q *RedisQueue) refreshDeadGauge(ctx context.Context) error {
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := writeRESP(rw, "LLEN", q.deadKey()); err != nil {
		return err
	}
	resp, err := readRESP(rw)
	if err != nil {
		return err
	}
	n, err := atoiRESP(resp)
	if err != nil {
		return err
	}
	observability.Default.SetGauge("dead_letter_count", q.labels(nil), float64(n))
	return nil
}

func (q *RedisQueue) pushDeadRaw(ctx context.Context, payload string) error {
	conn, rw, err := q.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := writeRESP(rw, "LPUSH", q.deadKey(), payload); err != nil {
		return err
	}
	_, err = readRESP(rw)
	return err
}

func (q *RedisQueue) getClaimPayload(rw *bufio.ReadWriter, receipt string) (string, error) {
	if err := writeRESP(rw, "HGET", q.claimsKey(), receipt); err != nil {
		return "", err
	}
	resp, err := readRESP(rw)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	s, ok := resp.(string)
	if !ok {
		return "", errors.New("unexpected redis payload type")
	}
	return s, nil
}

func (q *RedisQueue) incrNack(rw *bufio.ReadWriter, receipt string) (int, error) {
	if err := writeRESP(rw, "HINCRBY", q.nackKey(), receipt, "1"); err != nil {
		return 0, err
	}
	resp, err := readRESP(rw)
	if err != nil {
		return 0, err
	}
	s, ok := resp.(string)
	if !ok {
		return 0, errors.New("unexpected redis integer response")
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func (q *RedisQueue) connect(ctx context.Context) (net.Conn, *bufio.ReadWriter, error) {
	dialer := net.Dialer{Timeout: q.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", q.cfg.Addr)
	if err != nil {
		return nil, nil, err
	}
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if q.cfg.Password != "" {
		if err := writeRESP(rw, "AUTH", q.cfg.Password); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		if _, err := readRESP(rw); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}
	if q.cfg.DB > 0 {
		if err := writeRESP(rw, "SELECT", strconv.Itoa(q.cfg.DB)); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		if _, err := readRESP(rw); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}
	return conn, rw, nil
}

func writeRESP(rw *bufio.ReadWriter, parts ...string) error {
	if _, err := fmt.Fprintf(rw, "*%d\r\n", len(parts)); err != nil {
		return err
	}
	for _, p := range parts {
		if _, err := fmt.Fprintf(rw, "$%d\r\n%s\r\n", len(p), p); err != nil {
			return err
		}
	}
	return rw.Flush()
}

func readRESP(rw *bufio.ReadWriter) (any, error) {
	prefix, err := rw.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := rw.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")

	switch prefix {
	case '+', ':':
		return line, nil
	case '-':
		return nil, fmt.Errorf("redis error: %s", line)
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n == -1 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(rw, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if n == -1 {
			return nil, nil
		}
		arr := make([]string, 0, n)
		for i := 0; i < n; i++ {
			v, err := readRESP(rw)
			if err != nil {
				return nil, err
			}
			if v == nil {
				arr = append(arr, "")
				continue
			}
			s, ok := v.(string)
			if !ok {
				return nil, errors.New("unexpected redis array element")
			}
			arr = append(arr, s)
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unsupported redis response prefix %q", prefix)
	}
}

func toStringArray(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]string)
	if !ok {
		return nil, errors.New("unexpected redis array response type")
	}
	return arr, nil
}

func atoiRESP(v any) (int, error) {
	if v == nil {
		return 0, nil
	}
	s, ok := v.(string)
	if !ok {
		return 0, errors.New("unexpected redis integer response type")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func encodeTaskRef(ref TaskRef) string {
	return ref.JobID + "|" + ref.TaskID
}

func decodeTaskRef(raw string) (TaskRef, bool) {
	parts := strings.SplitN(raw, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return TaskRef{}, false
	}
	return TaskRef{JobID: parts[0], TaskID: parts[1]}, true
}
