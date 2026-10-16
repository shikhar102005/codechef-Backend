package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/seat-booking/system/internal/domain"
	"github.com/seat-booking/system/internal/messaging"
)

type Processor struct {
	repo domain.Repository
	bus  messaging.MessageBus
	stop chan struct{}
}

func NewProcessor(repo domain.Repository, bus messaging.MessageBus) *Processor {
	return &Processor{
		repo: repo,
		bus:  bus,
		stop: make(chan struct{}),
	}
}

func (p *Processor) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-p.stop:
				ticker.Stop()
				return
			case <-ticker.C:
				p.processPendingEvents()
			}
		}
	}()
}

func (p *Processor) Stop() {
	close(p.stop)
}

func (p *Processor) processPendingEvents() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := p.repo.FetchPendingOutboxEvents(ctx, 50)
	if err != nil || len(events) == 0 {
		return
	}

	for _, event := range events {
		var routingKey string
		switch event.AggregateType {
		case "RESERVATION":
			routingKey = messaging.QueueExpirations
		case "BOOKING":
			routingKey = messaging.QueueNotifications
		case "PAYMENT":
			routingKey = messaging.QueuePayments
		default:
			routingKey = messaging.QueueNotifications
		}

		err := p.bus.Publish(ctx, routingKey, event.EventType, event.Payload)
		if err != nil {
			slog.Error("Failed to publish outbox event to bus", "id", event.ID, "err", err)
			_ = p.repo.MarkOutboxEventFailed(ctx, event.ID)
		} else {
			_ = p.repo.MarkOutboxEventPublished(ctx, event.ID)
		}
	}
}
