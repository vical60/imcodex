package lark

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/magnaflowlabs/imcodex/internal/gateway"
)

const defaultPollInterval = time.Second

type ListedMessage struct {
	Message         gateway.IncomingMessage
	CreatedAtMillis int64
}

type MessageLister interface {
	ListChatMessagesSince(ctx context.Context, groupID string, startAtMillis int64) ([]ListedMessage, error)
}

type Poller struct {
	client   MessageLister
	handler  MessageHandler
	groupIDs []string
	logger   *slog.Logger
	interval time.Duration
	cursors  map[string]pollCursor
}

type pollCursor struct {
	createdAtMillis int64
	messageIDs      map[string]struct{}
}

func NewPoller(client MessageLister, groupIDs []string, handler MessageHandler, logger *slog.Logger) *Poller {
	if logger == nil {
		logger = slog.Default()
	}

	now := time.Now().UnixMilli()
	cursors := make(map[string]pollCursor, len(groupIDs))
	normalized := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		normalized = append(normalized, groupID)
		cursors[groupID] = pollCursor{
			createdAtMillis: now,
			messageIDs:      make(map[string]struct{}),
		}
	}

	return &Poller{
		client:   client,
		handler:  handler,
		groupIDs: normalized,
		logger:   logger,
		interval: defaultPollInterval,
		cursors:  cursors,
	}
}

func (p *Poller) Start(ctx context.Context) error {
	if p == nil || p.client == nil || p.handler == nil || len(p.groupIDs) == 0 {
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.pollAll(ctx)
		}
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	for _, groupID := range p.groupIDs {
		if err := p.pollGroup(ctx, groupID); err != nil && ctx.Err() == nil {
			p.logger.Warn("poll lark messages failed", "group_id", groupID, "err", err)
		}
	}
}

func (p *Poller) pollGroup(ctx context.Context, groupID string) error {
	cursor := p.cursors[groupID]
	listed, err := p.client.ListChatMessagesSince(ctx, groupID, cursor.createdAtMillis)
	if err != nil {
		return err
	}

	for _, item := range listed {
		if item.CreatedAtMillis < cursor.createdAtMillis {
			continue
		}
		if item.CreatedAtMillis == cursor.createdAtMillis {
			if _, exists := cursor.messageIDs[item.Message.MessageID]; exists {
				continue
			}
		}
		if err := p.handler.HandleMessage(ctx, item.Message); err != nil {
			return err
		}
		if item.CreatedAtMillis > cursor.createdAtMillis {
			cursor.createdAtMillis = item.CreatedAtMillis
			cursor.messageIDs = make(map[string]struct{})
		}
		if cursor.messageIDs == nil {
			cursor.messageIDs = make(map[string]struct{})
		}
		cursor.messageIDs[item.Message.MessageID] = struct{}{}
	}

	p.cursors[groupID] = cursor
	return nil
}
