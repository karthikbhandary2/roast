// Package kafkaclient wraps segmentio/kafka-go for the event bus. Two
// distinct usages live in this system, kept as separate small types rather
// than one do-everything client:
//   - Producer: services publish domain events (order.confirmed,
//     product.updated, inventory.changed, ...) after their own DB
//     transaction commits.
//   - Consumer: Search Service builds its index by consuming product/
//     inventory events; Notification Service consumes order/payment events
//     to trigger sends.
package kafkaclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// Event is the envelope every message on the bus uses, so every consumer
// can decode the envelope first and dispatch on Type before decoding the
// type-specific Payload.
type Event struct {
	Type          string          `json:"type"` // e.g. "order.confirmed", "product.updated"
	CorrelationID string          `json:"correlation_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

type Producer struct {
	writer *kafka.Writer
}

func NewProducer(brokers []string, topic string) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireAll, // wait for all in-sync replicas, not just the leader
			// Dev convenience: create the topic on first write. kafka-go won't do
			// this by default even when the broker allows it. In production,
			// create topics explicitly with the partition counts from LLD.md §4.5.
			AllowAutoTopicCreation: true,
		},
	}
}

// Publish sends an event keyed by aggregateID (e.g. the sub_order_id or
// product_id) so Kafka's partitioning keeps every event for the same
// aggregate in order on the same partition - important for a consumer like
// Search Service that must apply product.updated events in the order they
// happened.
func (p *Producer) Publish(ctx context.Context, aggregateID string, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("kafkaclient: encode payload: %w", err)
	}
	envelope := Event{
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
		Payload:    body,
	}
	envBytes, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("kafkaclient: encode envelope: %w", err)
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(aggregateID),
		Value: envBytes,
	})
}

func (p *Producer) Close() error {
	return p.writer.Close()
}

type Consumer struct {
	reader *kafka.Reader
}

func NewConsumer(brokers []string, topic, groupID string) *Consumer {
	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID, // consumer group -> Kafka tracks committed offsets per group, so a restart resumes rather than replaying the whole topic
		}),
	}
}

// Consume blocks, calling handler for each message and committing its
// offset only after handler returns nil - an unhandled error leaves the
// message uncommitted so it's redelivered on restart, rather than silently
// dropped. handler must therefore be idempotent (the same event may be
// delivered more than once, e.g. after a crash between processing and
// committing).
func (c *Consumer) Consume(ctx context.Context, handler func(context.Context, Event) error) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return fmt.Errorf("kafkaclient: fetch message: %w", err)
		}

		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			// A malformed message shouldn't wedge the whole consumer -
			// commit past it and log, rather than retrying forever.
			_ = c.reader.CommitMessages(ctx, msg)
			continue
		}

		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("kafkaclient: handler failed, offset not committed: %w", err)
		}
		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			return fmt.Errorf("kafkaclient: commit offset: %w", err)
		}
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}