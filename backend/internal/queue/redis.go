package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	StreamOrders = "stream:orders"
	GroupOrders  = "order-workers"
)

type RedisQueue struct {
	client *redis.Client
}

func NewRedisQueue(addr string) *RedisQueue {
	return &RedisQueue{client: redis.NewClient(&redis.Options{Addr: addr})}
}

func (q *RedisQueue) Client() *redis.Client { return q.client }

func (q *RedisQueue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

func (q *RedisQueue) Close() error { return q.client.Close() }

type StreamMessage struct {
	ID      string
	Payload json.RawMessage
}

func (q *RedisQueue) EnsureGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, StreamOrders, GroupOrders, "0").Err()
	if err == nil {
		return nil
	}
	if contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *RedisQueue) Publish(ctx context.Context, payload json.RawMessage) error {
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamOrders,
		Approx: true,
		MaxLen: 10000,
		Values: map[string]any{"payload": string(payload)},
	}).Err()
}

func (q *RedisQueue) Read(ctx context.Context, consumer string, count int, block time.Duration) ([]StreamMessage, error) {
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    GroupOrders,
		Consumer: consumer,
		Streams:  []string{StreamOrders, ">"},
		Count:    int64(count),
		Block:    block,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var out []StreamMessage
	for _, s := range streams {
		for _, m := range s.Messages {
			v, ok := m.Values["payload"]
			if !ok {
				continue
			}
			out = append(out, StreamMessage{ID: m.ID, Payload: json.RawMessage(fmt.Sprint(v))})
		}
	}
	return out, nil
}

func (q *RedisQueue) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	return q.client.XAck(ctx, StreamOrders, GroupOrders, ids...).Err()
}

func (q *RedisQueue) Length(ctx context.Context) (int64, error) {
	return q.client.XLen(ctx, StreamOrders).Result()
}

func contains(s, needle string) bool {
	return len(needle) == 0 || (len(s) >= len(needle) && find(s, needle) >= 0)
}

func find(s, needle string) int {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
