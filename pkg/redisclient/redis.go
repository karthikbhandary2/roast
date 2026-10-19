// Package redisclient wraps go-redis for the two things Redis is actually
// the right tool for in this system: the shopping cart (ephemeral,
// TTL-bound, fine to lose) and session/dedupe data. It is deliberately NOT
// used as the source of truth for anything money- or stock-related - see
// the idempotency package for why those live in Postgres instead.
package redisclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func New(addr, password string, db int) *Client {
	// If addr is a full redis connection URL (e.g. redis://user:pass@host:port),
	// parse it directly.
	if opt, err := redis.ParseURL(addr); err == nil {
		if password != "" {
			opt.Password = password
		}
		return &Client{rdb: redis.NewClient(opt)}
	}

	return &Client{
		rdb: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
	}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

// --- Cart ---
// Stored as a single JSON blob per buyer under cart:{buyer_id}, with a TTL
// matching the reservation-hold window shown on the cart/checkout screens.
// Losing a cart on a Redis restart is an acceptable failure mode (worst
// case: the buyer re-adds items) - this is exactly the kind of state that
// does NOT belong in a service-of-record database.

type CartItem struct {
	ProductID string `json:"product_id"`
	SellerID  string `json:"seller_id"`
	Quantity  int32  `json:"quantity"`
	UnitPrice int64  `json:"unit_price_amount_minor"`
	Currency  string `json:"currency"`
}

func cartKey(buyerID string) string {
	return fmt.Sprintf("cart:%s", buyerID)
}

func (c *Client) GetCart(ctx context.Context, buyerID string) ([]CartItem, error) {
	raw, err := c.rdb.Get(ctx, cartKey(buyerID)).Bytes()
	if err == redis.Nil {
		return []CartItem{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redisclient: get cart: %w", err)
	}
	var items []CartItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("redisclient: decode cart: %w", err)
	}
	return items, nil
}

func (c *Client) SetCart(ctx context.Context, buyerID string, items []CartItem, ttl time.Duration) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("redisclient: encode cart: %w", err)
	}
	if err := c.rdb.Set(ctx, cartKey(buyerID), raw, ttl).Err(); err != nil {
		return fmt.Errorf("redisclient: set cart: %w", err)
	}
	return nil
}

func (c *Client) DeleteCart(ctx context.Context, buyerID string) error {
	if err := c.rdb.Del(ctx, cartKey(buyerID)).Err(); err != nil {
		return fmt.Errorf("redisclient: delete cart: %w", err)
	}
	return nil
}

// CartTTL returns how much time is left before the cart (and its implicit
// checkout reservation window) expires - what the "reserved for 8:42 more"
// countdown on the cart wireframe reads from.
func (c *Client) CartTTL(ctx context.Context, buyerID string) (time.Duration, error) {
	ttl, err := c.rdb.TTL(ctx, cartKey(buyerID)).Result()
	if err != nil {
		return 0, fmt.Errorf("redisclient: cart ttl: %w", err)
	}
	return ttl, nil
}

// --- Low-stakes dedupe (e.g. Notification Service) ---
// A SETNX-based lock is fine here because a rare duplicate email is a
// low-cost failure - unlike a duplicate charge, which needs the stronger,
// database-backed guarantee in pkg/idempotency.

func (c *Client) AcquireOnce(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, "dedupe:"+key, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redisclient: acquire once: %w", err)
	}
	return ok, nil
}

// --- Pub/Sub (order status push - LLD.md §2.4/§3.11) ---
// Best-effort only: a subscriber that connects late misses earlier
// messages, and Redis Pub/Sub delivers nothing to a disconnected
// subscriber. That's an accepted trade-off here because polling
// GET /v1/orders/{id} remains the correctness fallback - this is a latency
// improvement, not the only path status ever reaches the client.

func (c *Client) PublishOrderStatus(ctx context.Context, orderID string, payload []byte) error {
	if err := c.rdb.Publish(ctx, "order-status:"+orderID, payload).Err(); err != nil {
		return fmt.Errorf("redisclient: publish order status: %w", err)
	}
	return nil
}

// SubscribeOrderStatus returns a channel of raw message payloads for one
// order. The caller must call the returned close func when done (e.g. when
// the SSE client disconnects) to release the subscription.
func (c *Client) SubscribeOrderStatus(ctx context.Context, orderID string) (<-chan []byte, func() error) {
	sub := c.rdb.Subscribe(ctx, "order-status:"+orderID)
	out := make(chan []byte)
	go func() {
		defer close(out)
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				out <- []byte(msg.Payload)
			}
		}
	}()
	return out, sub.Close
}
