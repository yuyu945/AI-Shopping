package analytics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/segmentio/kafka-go"
)

type ReviewEventHandler interface {
	HandleReviewEvent(context.Context, ReviewEvent) error
}

type BehaviorEventHandler interface {
	HandleBehaviorEvent(context.Context, BehaviorEvent) error
	RecordDeadLetter(context.Context, DeadLetterRecord) error
}

type reviewMessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
	Close() error
}

type reviewDeadLetterPublisher interface {
	Publish(context.Context, kafka.Message) error
	Close() error
}

type kafkaDeadLetterPublisher struct {
	writer *kafka.Writer
}

func (p *kafkaDeadLetterPublisher) Publish(ctx context.Context, message kafka.Message) error {
	return p.writer.WriteMessages(ctx, message)
}

func (p *kafkaDeadLetterPublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

type ReviewConsumer struct {
	reader      reviewMessageReader
	handler     ReviewEventHandler
	dlq         reviewDeadLetterPublisher
	callTimeout time.Duration
}

type BehaviorConsumer struct {
	reader      reviewMessageReader
	handler     BehaviorEventHandler
	dlq         reviewDeadLetterPublisher
	callTimeout time.Duration
}

func NewReviewConsumer(brokers []string, handler ReviewEventHandler, callTimeout time.Duration) *ReviewConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: ReviewConsumerGroup, Topic: ReviewEventsTopic, MinBytes: 1, MaxBytes: 1e6})
	publisher := &kafkaDeadLetterPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
	return newReviewConsumer(reader, handler, publisher, callTimeout)
}

func NewBehaviorConsumer(brokers []string, handler BehaviorEventHandler, callTimeout time.Duration) *BehaviorConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: BehaviorConsumerGroup, Topic: BehaviorEventsTopic, MinBytes: 1, MaxBytes: 1e6})
	publisher := &kafkaDeadLetterPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
	return newBehaviorConsumer(reader, handler, publisher, callTimeout)
}

func newReviewConsumer(reader reviewMessageReader, handler ReviewEventHandler, dlq reviewDeadLetterPublisher, callTimeout time.Duration) *ReviewConsumer {
	return &ReviewConsumer{reader: reader, handler: handler, dlq: dlq, callTimeout: callTimeout}
}

func newBehaviorConsumer(reader reviewMessageReader, handler BehaviorEventHandler, dlq reviewDeadLetterPublisher, callTimeout time.Duration) *BehaviorConsumer {
	return &BehaviorConsumer{reader: reader, handler: handler, dlq: dlq, callTimeout: callTimeout}
}

func (c *ReviewConsumer) Run(ctx context.Context) error {
	if c == nil || c.reader == nil || c.handler == nil || c.dlq == nil || c.callTimeout <= 0 {
		return errors.New("review analytics consumer is unavailable")
	}
	for {
		fetchCtx, cancelFetch := c.callContext(ctx)
		message, err := c.reader.FetchMessage(fetchCtx)
		cancelFetch()
		if err != nil {
			return err
		}
		if err := c.handleMessage(ctx, message); err != nil {
			return err
		}
	}
}

func (c *ReviewConsumer) Close() error {
	if c == nil {
		return nil
	}
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			return err
		}
	}
	if c.dlq != nil {
		return c.dlq.Close()
	}
	return nil
}

func (c *BehaviorConsumer) Run(ctx context.Context) error {
	if c == nil || c.reader == nil || c.handler == nil || c.dlq == nil || c.callTimeout <= 0 {
		return errors.New("behavior analytics consumer is unavailable")
	}
	for {
		fetchCtx, cancelFetch := c.callContext(ctx)
		message, err := c.reader.FetchMessage(fetchCtx)
		cancelFetch()
		if err != nil {
			return err
		}
		if err := c.handleMessage(ctx, message); err != nil {
			return err
		}
	}
}

func (c *BehaviorConsumer) Close() error {
	if c == nil {
		return nil
	}
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			return err
		}
	}
	if c.dlq != nil {
		return c.dlq.Close()
	}
	return nil
}

func (c *ReviewConsumer) handleMessage(ctx context.Context, message kafka.Message) error {
	var event ReviewEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return c.deadLetterAndCommit(ctx, message, "malformed_review_event")
	}
	if err := event.Validate(); err != nil {
		return c.deadLetterAndCommit(ctx, message, "invalid_review_event")
	}
	handleCtx, cancelHandle := c.callContext(ctx)
	err := c.handler.HandleReviewEvent(handleCtx, event)
	cancelHandle()
	if err != nil {
		return err
	}
	commitCtx, cancelCommit := c.callContext(ctx)
	err = c.reader.CommitMessages(commitCtx, message)
	cancelCommit()
	return err
}

func (c *BehaviorConsumer) handleMessage(ctx context.Context, message kafka.Message) error {
	var event BehaviorEvent
	if err := json.Unmarshal(message.Value, &event); err != nil {
		return c.deadLetterAndCommit(ctx, message, "malformed_behavior_event")
	}
	if err := event.Validate(); err != nil {
		return c.deadLetterAndCommit(ctx, message, "invalid_behavior_event")
	}
	handleCtx, cancelHandle := c.callContext(ctx)
	err := c.handler.HandleBehaviorEvent(handleCtx, event)
	cancelHandle()
	if err != nil {
		return err
	}
	commitCtx, cancelCommit := c.callContext(ctx)
	err = c.reader.CommitMessages(commitCtx, message)
	cancelCommit()
	return err
}

func (c *ReviewConsumer) deadLetterAndCommit(ctx context.Context, message kafka.Message, reason string) error {
	payload, err := json.Marshal(struct {
		Reason         string `json:"reason"`
		RawEventBase64 string `json:"raw_event_base64"`
	}{Reason: reason, RawEventBase64: base64.StdEncoding.EncodeToString(message.Value)})
	if err != nil {
		return errors.New("marshal review dead-letter event failed")
	}
	publishCtx, cancelPublish := c.callContext(ctx)
	err = c.dlq.Publish(publishCtx, kafka.Message{Topic: ReviewEventsDeadTopic, Key: message.Key, Value: payload})
	cancelPublish()
	if err != nil {
		return errors.New("publish review dead-letter event failed")
	}
	commitCtx, cancelCommit := c.callContext(ctx)
	err = c.reader.CommitMessages(commitCtx, message)
	cancelCommit()
	return err
}

func (c *BehaviorConsumer) deadLetterAndCommit(ctx context.Context, message kafka.Message, reason string) error {
	rawEvent := base64.StdEncoding.EncodeToString(message.Value)
	payload, err := json.Marshal(struct {
		Reason         string `json:"reason"`
		RawEventBase64 string `json:"raw_event_base64"`
	}{Reason: reason, RawEventBase64: rawEvent})
	if err != nil {
		return errors.New("marshal behavior dead-letter event failed")
	}
	publishCtx, cancelPublish := c.callContext(ctx)
	err = c.dlq.Publish(publishCtx, kafka.Message{Topic: BehaviorEventsDeadTopic, Key: message.Key, Value: payload})
	cancelPublish()
	if err != nil {
		return errors.New("publish behavior dead-letter event failed")
	}
	recordCtx, cancelRecord := c.callContext(ctx)
	err = c.handler.RecordDeadLetter(recordCtx, DeadLetterRecord{
		Topic: BehaviorEventsTopic, EventKey: string(message.Key), Reason: reason, RawEventBase64: rawEvent,
	})
	cancelRecord()
	if err != nil {
		return err
	}
	commitCtx, cancelCommit := c.callContext(ctx)
	err = c.reader.CommitMessages(commitCtx, message)
	cancelCommit()
	return err
}

func (c *ReviewConsumer) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.callTimeout)
}

func (c *BehaviorConsumer) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.callTimeout)
}
