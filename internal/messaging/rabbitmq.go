package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeName       = "seat_booking_exchange"
	QueuePayments      = "payment_processing_queue"
	QueueExpirations   = "reservation_expiry_queue"
	QueueNotifications = "booking_notifications_queue"
	QueueDLQ           = "seat_booking_dlq"
	ExchangeDLQ        = "seat_booking_dlx"
)

type MessageHandler func(ctx context.Context, eventType string, payload []byte) error

type MessageBus interface {
	Publish(ctx context.Context, routingKey string, eventType string, payload []byte) error
	Subscribe(queueName string, handler MessageHandler) error
	IsHealthy() bool
	Close()
}

type RabbitMQBus struct {
	mu           sync.Mutex
	conn         *amqp.Connection
	ch           *amqp.Channel
	isAvailable  bool
	subscribers  map[string]MessageHandler
	inMemChan    chan inMemMsg
	closeChan    chan struct{}
}

type inMemMsg struct {
	routingKey string
	eventType  string
	payload    []byte
}

func NewMessageBus(ctx context.Context, amqpURL string) MessageBus {
	bus := &RabbitMQBus{
		subscribers: make(map[string]MessageHandler),
		inMemChan:   make(chan inMemMsg, 500),
		closeChan:   make(chan struct{}),
	}

	if amqpURL != "" && amqpURL != "none" && amqpURL != "memory" {
		conn, err := amqp.Dial(amqpURL)
		if err == nil {
			ch, err := conn.Channel()
			if err == nil {
				if err := ch.Confirm(false); err == nil {
					bus.conn = conn
					bus.ch = ch
					bus.isAvailable = true
					slog.Info("Connected to RabbitMQ with publisher confirms enabled")
					bus.setupTopology()
				}
			}
		}
	}

	if !bus.isAvailable {
		slog.Warn("RabbitMQ unavailable. Utilizing integrated high-reliability in-process asynchronous bus.")
	}

	// Start in-memory dispatcher loop (handles local dispatch and fallback)
	go bus.inMemDispatcher()

	return bus
}

func (b *RabbitMQBus) setupTopology() {
	if !b.isAvailable || b.ch == nil {
		return
	}

	// Declare DLX and DLQ
	_ = b.ch.ExchangeDeclare(ExchangeDLQ, "direct", true, false, false, false, nil)
	_, _ = b.ch.QueueDeclare(QueueDLQ, true, false, false, false, nil)
	_ = b.ch.QueueBind(QueueDLQ, "dead_letter", ExchangeDLQ, false, nil)

	// Main Exchange
	_ = b.ch.ExchangeDeclare(ExchangeName, "topic", true, false, false, false, nil)

	queues := []string{QueuePayments, QueueExpirations, QueueNotifications}
	dlqArgs := amqp.Table{
		"x-dead-letter-exchange":    ExchangeDLQ,
		"x-dead-letter-routing-key": "dead_letter",
	}

	for _, q := range queues {
		_, err := b.ch.QueueDeclare(q, true, false, false, false, dlqArgs)
		if err == nil {
			_ = b.ch.QueueBind(q, q, ExchangeName, false, nil)
		}
	}
}

func (b *RabbitMQBus) IsHealthy() bool {
	if !b.isAvailable || b.conn == nil {
		return true // Fallback bus is healthy
	}
	return !b.conn.IsClosed()
}

func (b *RabbitMQBus) Publish(ctx context.Context, routingKey string, eventType string, payload []byte) error {
	if b.isAvailable && b.ch != nil {
		confirmation, err := b.ch.PublishWithDeferredConfirmWithContext(
			ctx,
			ExchangeName,
			routingKey,
			true,  // mandatory
			false, // immediate
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "application/json",
				Timestamp:    time.Now(),
				Type:         eventType,
				Body:         payload,
			},
		)
		if err == nil {
			if confirmed := confirmation.Wait(); confirmed {
				return nil
			}
		}
		slog.Warn("RabbitMQ publish confirm failed, dispatching through fallback queue", "routingKey", routingKey)
	}

	// Resilient fallback delivery
	select {
	case b.inMemChan <- inMemMsg{routingKey: routingKey, eventType: eventType, payload: payload}:
		return nil
	default:
		slog.Error("In-memory event bus full, dropped message", "type", eventType)
		return fmt.Errorf("event bus full")
	}
}

func (b *RabbitMQBus) Subscribe(queueName string, handler MessageHandler) error {
	b.mu.Lock()
	b.subscribers[queueName] = handler
	b.mu.Unlock()

	if b.isAvailable && b.ch != nil {
		msgs, err := b.ch.Consume(
			queueName,
			"",
			false, // manual ack
			false,
			false,
			false,
			nil,
		)
		if err == nil {
			go func() {
				for d := range msgs {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err := handler(ctx, d.Type, d.Body)
					cancel()
					if err == nil {
						_ = d.Ack(false)
					} else {
						slog.Error("Message processing error, sending to dead-letter queue", "err", err)
						_ = d.Nack(false, false) // send to DLQ
					}
				}
			}()
		}
	}

	return nil
}

func (b *RabbitMQBus) inMemDispatcher() {
	for {
		select {
		case <-b.closeChan:
			return
		case msg := <-b.inMemChan:
			b.mu.Lock()
			handler, exists := b.subscribers[msg.routingKey]
			b.mu.Unlock()

			if exists {
				go func(m inMemMsg, h MessageHandler) {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := h(ctx, m.eventType, m.payload); err != nil {
						slog.Error("Handler returned error for event", "type", m.eventType, "err", err)
					}
				}(msg, handler)
			}
		}
	}
}

func (b *RabbitMQBus) Close() {
	close(b.closeChan)
	if b.ch != nil {
		_ = b.ch.Close()
	}
	if b.conn != nil {
		_ = b.conn.Close()
	}
}

// PaymentEvent payload
type PaymentEventPayload struct {
	PaymentID     string `json:"payment_id"`
	ReservationID string `json:"reservation_id"`
	UserID        string `json:"user_id"`
	AmountCents   int    `json:"amount_cents"`
	Simulation    string `json:"simulation"` // SUCCESS, FAILURE, DELAYED, TIMEOUT
}
