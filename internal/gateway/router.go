package gateway

import (
	"context"
	"fmt"
	"log/slog"
)

type Router struct {
	services map[string]*Service
}

func NewRouter(ctx context.Context, groups []Options, messenger Messenger, console Console, logger *slog.Logger) (*Router, error) {
	if logger == nil {
		logger = slog.Default()
	}

	services := make(map[string]*Service, len(groups))
	for _, group := range groups {
		if _, exists := services[group.GroupID]; exists {
			return nil, fmt.Errorf("duplicate group id: %s", group.GroupID)
		}
		services[group.GroupID] = NewService(ctx, group, messenger, console, logger)
	}
	return &Router{services: services}, nil
}

func (r *Router) HandleMessage(ctx context.Context, msg IncomingMessage) error {
	service := r.services[msg.GroupID]
	if service == nil {
		return nil
	}
	return service.HandleMessage(ctx, msg)
}

func (r *Router) GroupCount() int {
	return len(r.services)
}
