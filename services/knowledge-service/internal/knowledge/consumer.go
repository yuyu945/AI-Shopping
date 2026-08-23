package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/segmentio/kafka-go"
)

const knowledgeDeadLetterSuffix = ".deadletter"

type DocumentIngestHandler interface {
	HandleDocumentIngest(context.Context, IngestEvent) error
}

type ChunkEmbedHandler interface {
	HandleChunkEmbed(context.Context, ChunkEmbedEvent) error
}

type knowledgeMessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
	Close() error
}

type knowledgeDeadLetterPublisher interface {
	Publish(context.Context, kafka.Message) error
	Close() error
}

type kafkaKnowledgeDeadLetterPublisher struct{ writer *kafka.Writer }

func (p *kafkaKnowledgeDeadLetterPublisher) Publish(ctx context.Context, message kafka.Message) error {
	return p.writer.WriteMessages(ctx, message)
}

func (p *kafkaKnowledgeDeadLetterPublisher) Close() error { return p.writer.Close() }

type KafkaKnowledgeConsumer struct {
	reader      knowledgeMessageReader
	handler     any
	dlq         knowledgeDeadLetterPublisher
	callTimeout time.Duration
}

func NewKafkaDocumentIngestConsumer(brokers []string, handler DocumentIngestHandler, callTimeout time.Duration) *KafkaKnowledgeConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: documentIngestConsumerGroup, Topic: documentIngestTopic, MinBytes: 1, MaxBytes: 1e6})
	publisher := &kafkaKnowledgeDeadLetterPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
	return newKafkaKnowledgeConsumerWithTimeout(reader, handler, publisher, callTimeout)
}

func NewKafkaChunkEmbedConsumer(brokers []string, handler ChunkEmbedHandler, callTimeout time.Duration) *KafkaKnowledgeConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: chunkEmbedConsumerGroup, Topic: chunkEmbedTopic, MinBytes: 1, MaxBytes: 1e6})
	publisher := &kafkaKnowledgeDeadLetterPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
	return newKafkaKnowledgeConsumerWithTimeout(reader, handler, publisher, callTimeout)
}

func newKafkaKnowledgeConsumer(reader knowledgeMessageReader, handler any, dlq knowledgeDeadLetterPublisher) *KafkaKnowledgeConsumer {
	return newKafkaKnowledgeConsumerWithTimeout(reader, handler, dlq, time.Second)
}

func newKafkaKnowledgeConsumerWithTimeout(reader knowledgeMessageReader, handler any, dlq knowledgeDeadLetterPublisher, callTimeout time.Duration) *KafkaKnowledgeConsumer {
	return &KafkaKnowledgeConsumer{reader: reader, handler: handler, dlq: dlq, callTimeout: callTimeout}
}

func (c *KafkaKnowledgeConsumer) Close() error {
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

func (c *KafkaKnowledgeConsumer) Run(ctx context.Context) error {
	if c == nil || c.reader == nil || c.handler == nil || c.dlq == nil || c.callTimeout <= 0 {
		return errors.New("knowledge kafka consumer is unavailable")
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

func (c *KafkaKnowledgeConsumer) handleMessage(ctx context.Context, message kafka.Message) error {
	switch message.Topic {
	case documentIngestTopic:
		handler, ok := c.handler.(DocumentIngestHandler)
		if !ok {
			return errors.New("knowledge ingest handler is unavailable")
		}
		var event IngestEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			return c.deadLetterAndCommit(ctx, message, "malformed_knowledge_event")
		}
		handleCtx, cancel := c.callContext(ctx)
		err := handler.HandleDocumentIngest(handleCtx, event)
		cancel()
		if err != nil {
			return err
		}
	case chunkEmbedTopic:
		handler, ok := c.handler.(ChunkEmbedHandler)
		if !ok {
			return errors.New("knowledge embed handler is unavailable")
		}
		var event ChunkEmbedEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			return c.deadLetterAndCommit(ctx, message, "malformed_knowledge_event")
		}
		handleCtx, cancel := c.callContext(ctx)
		err := handler.HandleChunkEmbed(handleCtx, event)
		cancel()
		if err != nil {
			return err
		}
	default:
		return errors.New("knowledge kafka handler is unavailable")
	}
	commitCtx, cancelCommit := c.callContext(ctx)
	err := c.reader.CommitMessages(commitCtx, message)
	cancelCommit()
	return err
}

func (c *KafkaKnowledgeConsumer) deadLetterAndCommit(ctx context.Context, message kafka.Message, reason string) error {
	payload, err := json.Marshal(struct {
		Reason         string `json:"reason"`
		RawEventBase64 string `json:"raw_event_base64"`
	}{Reason: reason, RawEventBase64: base64.StdEncoding.EncodeToString(message.Value)})
	if err != nil {
		return errors.New("marshal knowledge dead-letter event failed")
	}
	publishCtx, cancelPublish := c.callContext(ctx)
	err = c.dlq.Publish(publishCtx, kafka.Message{Topic: message.Topic + knowledgeDeadLetterSuffix, Key: message.Key, Value: payload})
	cancelPublish()
	if err != nil {
		return errors.New("publish knowledge dead-letter event failed")
	}
	commitCtx, cancelCommit := c.callContext(ctx)
	err = c.reader.CommitMessages(commitCtx, message)
	cancelCommit()
	return err
}

func (c *KafkaKnowledgeConsumer) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.callTimeout)
}
