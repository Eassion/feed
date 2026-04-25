package mq

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"feed/internal/config"
	canalclient "github.com/withlin/canal-go/client"
	pbe "github.com/withlin/canal-go/protocol/entry"
	"google.golang.org/protobuf/proto"
)

type CountCanalStrategy func(entry pbe.Entry, action string, before, after map[string]string, rowIndex int) (*CountCanalEvent, bool, error)

type CanalManager struct {
	cfg        config.CanalConfig
	kafka      *Manager
	topic      string
	logger     *slog.Logger
	strategies map[string]CountCanalStrategy
	connector  canalclient.CanalConnector
	wg         sync.WaitGroup
}

func NewCanalManager(cfg config.CanalConfig, kafka *Manager, topic string, logger *slog.Logger) *CanalManager {
	return &CanalManager{
		cfg:        cfg,
		kafka:      kafka,
		topic:      topic,
		logger:     logger,
		strategies: make(map[string]CountCanalStrategy),
	}
}

func (m *CanalManager) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

func (m *CanalManager) RegisterStrategy(table string, strategy CountCanalStrategy) {
	if m == nil || table == "" || strategy == nil {
		return
	}
	m.strategies[table] = strategy
}

func RegisterDefaultCanalStrategies(manager *CanalManager) {
	if manager == nil {
		return
	}

	manager.RegisterStrategy("ran_feed_like", func(entry pbe.Entry, action string, before, after map[string]string, rowIndex int) (*CountCanalEvent, bool, error) {
		contentID := parseEventInt64(after["content_id"], before["content_id"])
		userID := parseEventInt64(after["user_id"], before["user_id"])
		return &CountCanalEvent{
			EventID:    newCanalEventID(entry.GetHeader().GetLogfileName(), entry.GetHeader().GetLogfileOffset(), "ran_feed_like", action, rowIndex),
			Schema:     entry.GetHeader().GetSchemaName(),
			Table:      "ran_feed_like",
			Action:     action,
			ContentID:  contentID,
			UserID:     userID,
			AuthorID:   parseEventInt64(after["content_user_id"], before["content_user_id"]),
			OccurredAt: time.UnixMilli(entry.GetHeader().GetExecuteTime()),
			Logfile:    entry.GetHeader().GetLogfileName(),
			LogOffset:  entry.GetHeader().GetLogfileOffset(),
			Before:     before,
			After:      after,
		}, true, nil
	})

	manager.RegisterStrategy("ran_feed_favorite", func(entry pbe.Entry, action string, before, after map[string]string, rowIndex int) (*CountCanalEvent, bool, error) {
		contentID := parseEventInt64(after["content_id"], before["content_id"])
		userID := parseEventInt64(after["user_id"], before["user_id"])
		return &CountCanalEvent{
			EventID:    newCanalEventID(entry.GetHeader().GetLogfileName(), entry.GetHeader().GetLogfileOffset(), "ran_feed_favorite", action, rowIndex),
			Schema:     entry.GetHeader().GetSchemaName(),
			Table:      "ran_feed_favorite",
			Action:     action,
			ContentID:  contentID,
			UserID:     userID,
			AuthorID:   parseEventInt64(after["content_user_id"], before["content_user_id"]),
			OccurredAt: time.UnixMilli(entry.GetHeader().GetExecuteTime()),
			Logfile:    entry.GetHeader().GetLogfileName(),
			LogOffset:  entry.GetHeader().GetLogfileOffset(),
			Before:     before,
			After:      after,
		}, true, nil
	})

	manager.RegisterStrategy("ran_feed_comment", func(entry pbe.Entry, action string, before, after map[string]string, rowIndex int) (*CountCanalEvent, bool, error) {
		contentID := parseEventInt64(after["content_id"], before["content_id"])
		userID := parseEventInt64(after["user_id"], before["user_id"])
		return &CountCanalEvent{
			EventID:    newCanalEventID(entry.GetHeader().GetLogfileName(), entry.GetHeader().GetLogfileOffset(), "ran_feed_comment", action, rowIndex),
			Schema:     entry.GetHeader().GetSchemaName(),
			Table:      "ran_feed_comment",
			Action:     action,
			ContentID:  contentID,
			UserID:     userID,
			AuthorID:   parseEventInt64(after["content_user_id"], before["content_user_id"]),
			OccurredAt: time.UnixMilli(entry.GetHeader().GetExecuteTime()),
			Logfile:    entry.GetHeader().GetLogfileName(),
			LogOffset:  entry.GetHeader().GetLogfileOffset(),
			Before:     before,
			After:      after,
		}, true, nil
	})

	manager.RegisterStrategy("ran_feed_follow", func(entry pbe.Entry, action string, before, after map[string]string, rowIndex int) (*CountCanalEvent, bool, error) {
		return &CountCanalEvent{
			EventID:    newCanalEventID(entry.GetHeader().GetLogfileName(), entry.GetHeader().GetLogfileOffset(), "ran_feed_follow", action, rowIndex),
			Schema:     entry.GetHeader().GetSchemaName(),
			Table:      "ran_feed_follow",
			Action:     action,
			UserID:     parseEventInt64(after["user_id"], before["user_id"]),
			FolloweeID: parseEventInt64(after["follow_user_id"], before["follow_user_id"]),
			OccurredAt: time.UnixMilli(entry.GetHeader().GetExecuteTime()),
			Logfile:    entry.GetHeader().GetLogfileName(),
			LogOffset:  entry.GetHeader().GetLogfileOffset(),
			Before:     before,
			After:      after,
		}, true, nil
	})

	manager.RegisterStrategy("ran_feed_content", func(entry pbe.Entry, action string, before, after map[string]string, rowIndex int) (*CountCanalEvent, bool, error) {
		if !isDeletedContentTransition(action, before, after) {
			return nil, false, nil
		}

		return &CountCanalEvent{
			EventID:    newCanalEventID(entry.GetHeader().GetLogfileName(), entry.GetHeader().GetLogfileOffset(), "ran_feed_content", action, rowIndex),
			Schema:     entry.GetHeader().GetSchemaName(),
			Table:      "ran_feed_content",
			Action:     action,
			ContentID:  parseEventInt64(after["id"], before["id"]),
			AuthorID:   parseEventInt64(after["user_id"], before["user_id"]),
			Status:     firstNonEmptyValue(after["status"], before["status"]),
			OccurredAt: time.UnixMilli(entry.GetHeader().GetExecuteTime()),
			Logfile:    entry.GetHeader().GetLogfileName(),
			LogOffset:  entry.GetHeader().GetLogfileOffset(),
			Before:     before,
			After:      after,
		}, true, nil
	})
}

func (m *CanalManager) Start(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	if m.kafka == nil || !m.kafka.Enabled() {
		return fmt.Errorf("canal requires kafka producer to be enabled")
	}

	host, port, err := parseCanalAddr(m.cfg.Addr)
	if err != nil {
		return err
	}

	connector := canalclient.NewSimpleCanalConnector(
		host,
		port,
		m.cfg.Username,
		m.cfg.Password,
		m.cfg.Destination,
		60000,
		60*60*1000,
	)
	if err := connector.Connect(); err != nil {
		return err
	}
	if err := connector.Subscribe(m.cfg.Subscribe); err != nil {
		_ = connector.DisConnection()
		return err
	}

	m.connector = connector
	m.wg.Add(1)
	go m.consume(ctx)

	if m.logger != nil {
		m.logger.Info("canal subscriber started", "addr", m.cfg.Addr, "destination", m.cfg.Destination, "topic", m.topic, "filter", m.cfg.Subscribe)
	}
	return nil
}

func (m *CanalManager) consume(ctx context.Context) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		message, err := m.connector.GetWithOutAck(int32(m.cfg.BatchSize), nil, nil)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("read canal batch failed", "error", err)
			}
			_ = m.reconnect()
			time.Sleep(m.cfg.ReconnectInterval)
			continue
		}
		if message == nil || message.Id == -1 || len(message.Entries) == 0 {
			time.Sleep(m.cfg.PollInterval)
			continue
		}

		if err := m.handleEntries(ctx, message.Entries); err != nil {
			_ = m.connector.RollBack(message.Id)
			if m.logger != nil {
				m.logger.Error("handle canal batch failed", "batch_id", message.Id, "error", err)
			}
			time.Sleep(m.cfg.ReconnectInterval)
			continue
		}

		if err := m.connector.Ack(message.Id); err != nil && m.logger != nil {
			m.logger.Error("ack canal batch failed", "batch_id", message.Id, "error", err)
		}
	}
}

func (m *CanalManager) handleEntries(ctx context.Context, entries []pbe.Entry) error {
	for _, entry := range entries {
		if entry.GetEntryType() == pbe.EntryType_TRANSACTIONBEGIN || entry.GetEntryType() == pbe.EntryType_TRANSACTIONEND {
			continue
		}
		if entry.GetEntryType() != pbe.EntryType_ROWDATA {
			continue
		}

		rowChange := new(pbe.RowChange)
		if err := proto.Unmarshal(entry.GetStoreValue(), rowChange); err != nil {
			return err
		}

		action := rowChange.GetEventType().String()
		strategy, ok := m.strategies[entry.GetHeader().GetTableName()]
		if !ok {
			continue
		}

		for rowIndex, rowData := range rowChange.GetRowDatas() {
			before := columnsToMap(rowData.GetBeforeColumns())
			after := columnsToMap(rowData.GetAfterColumns())
			event, publish, err := strategy(entry, action, before, after, rowIndex)
			if err != nil {
				return err
			}
			if !publish || event == nil {
				continue
			}
			if err := m.kafka.PublishJSON(ctx, m.topic, event.Key(), event); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *CanalManager) reconnect() error {
	if m.connector != nil {
		_ = m.connector.DisConnection()
	}

	host, port, err := parseCanalAddr(m.cfg.Addr)
	if err != nil {
		return err
	}
	connector := canalclient.NewSimpleCanalConnector(
		host,
		port,
		m.cfg.Username,
		m.cfg.Password,
		m.cfg.Destination,
		60000,
		60*60*1000,
	)
	if err := connector.Connect(); err != nil {
		return err
	}
	if err := connector.Subscribe(m.cfg.Subscribe); err != nil {
		_ = connector.DisConnection()
		return err
	}
	m.connector = connector
	return nil
}

func (m *CanalManager) Close() error {
	if m == nil {
		return nil
	}
	if m.connector != nil {
		_ = m.connector.DisConnection()
	}
	m.wg.Wait()
	return nil
}

func parseCanalAddr(addr string) (string, int, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func columnsToMap(columns []*pbe.Column) map[string]string {
	result := make(map[string]string, len(columns))
	for _, column := range columns {
		if column == nil {
			continue
		}
		result[strings.ToLower(column.GetName())] = column.GetValue()
	}
	return result
}

func isDeletedContentTransition(action string, before, after map[string]string) bool {
	switch action {
	case ActionDelete:
		return isContentDeleted(before)
	case ActionUpdate:
		return !isContentDeleted(before) && isContentDeleted(after)
	default:
		return false
	}
}

func isContentDeleted(values map[string]string) bool {
	if len(values) == 0 {
		return false
	}
	if parseEventInt64(values["status"]) == 90 {
		return true
	}
	return parseEventBool(values["is_deleted"])
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
