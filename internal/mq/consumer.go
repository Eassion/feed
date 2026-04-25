package mq

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"feed/internal/config"
)

type MessageHandler func(ctx context.Context, msg kafka.Message) error

type Manager struct {
	cfg      config.KafkaConfig
	logger   *slog.Logger
	handlers map[string]MessageHandler
	readers  []*kafka.Reader
	writers  map[string]*kafka.Writer
	mu       sync.Mutex
	wg       sync.WaitGroup
}

func NewManager(cfg config.KafkaConfig, logger *slog.Logger) *Manager {
	return &Manager{
		cfg:      cfg,
		logger:   logger,
		handlers: make(map[string]MessageHandler),
		writers:  make(map[string]*kafka.Writer),
	}
}

func (m *Manager) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

func (m *Manager) Register(topic string, handler MessageHandler) {
	if m == nil || topic == "" || handler == nil {
		return
	}

	m.handlers[topic] = handler
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil || !m.cfg.Enabled {
		return nil
	}

	if len(m.cfg.Brokers) == 0 {
		return errors.New("kafka enabled but no brokers configured")
	}

	for topic, handler := range m.handlers {
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers: m.cfg.Brokers,
			GroupID: m.cfg.GroupID,
			Topic:   topic,
			MaxWait: 500 * time.Millisecond,
		})

		m.readers = append(m.readers, reader)
		m.wg.Add(1)

		go m.consume(ctx, reader, topic, handler)
	}

	m.logger.Info("kafka consumers started", "topics", len(m.handlers), "group_id", m.cfg.GroupID)
	return nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}

	var firstErr error
	for _, reader := range m.readers {
		if err := reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, writer := range m.writers {
		if err := writer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	m.wg.Wait()
	return firstErr
}

func (m *Manager) Publish(ctx context.Context, topic, key string, value []byte) error {
	if m == nil || !m.cfg.Enabled {
		return nil
	}
	if len(m.cfg.Brokers) == 0 {
		return errors.New("kafka enabled but no brokers configured")
	}

	m.mu.Lock()
	writer, ok := m.writers[topic]
	if !ok {
		writer = &kafka.Writer{
			Addr:         kafka.TCP(m.cfg.Brokers...),
			Topic:        topic,
			RequiredAcks: kafka.RequireOne,
			Balancer:     &kafka.Hash{},
		}
		m.writers[topic] = writer
	}
	m.mu.Unlock()

	return writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
		Time:  time.Now(),
	})
}

func (m *Manager) PublishJSON(ctx context.Context, topic, key string, payload any) error {
	if m == nil || !m.cfg.Enabled {
		return nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return m.Publish(ctx, topic, key, data)
}

func (m *Manager) consume(ctx context.Context, reader *kafka.Reader, topic string, handler MessageHandler) {
	defer m.wg.Done()

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}

			m.logger.Error("fetch kafka message failed", "topic", topic, "error", err)
			return
		}

		if err := handler(ctx, msg); err != nil {
			m.logger.Error("handle kafka message failed", "topic", topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
			continue
		}

		if err := reader.CommitMessages(ctx, msg); err != nil {
			m.logger.Error("commit kafka message failed", "topic", topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
		}
	}
}
