package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type AdmissionControl struct {
	maxConcurrency int
	queueCapacity  int
	queueTimeout   time.Duration
	semaphore      chan struct{}
	waitingQueue   chan chan bool
	mu             sync.Mutex
	activeCount    int
	waitingCount   int
}

func NewAdmissionControl(maxConcurrency, queueCapacity int, timeout time.Duration) *AdmissionControl {
	if maxConcurrency <= 0 {
		maxConcurrency = 50
	}
	if queueCapacity <= 0 {
		queueCapacity = 200
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	ac := &AdmissionControl{
		maxConcurrency: maxConcurrency,
		queueCapacity:  queueCapacity,
		queueTimeout:   timeout,
		semaphore:      make(chan struct{}, maxConcurrency),
		waitingQueue:   make(chan chan bool, queueCapacity),
	}

	go ac.queueDispatcher()
	return ac
}

func (ac *AdmissionControl) queueDispatcher() {
	for waitChan := range ac.waitingQueue {
		// Wait until a slot in semaphore opens up
		ac.semaphore <- struct{}{}
		// Grant permit to queued request
		waitChan <- true
	}
}

func (ac *AdmissionControl) Stats() (active int, queued int, max int, cap int) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.activeCount, ac.waitingCount, ac.maxConcurrency, ac.queueCapacity
}

// Handler returns a Gin middleware enforcing admission control
func (ac *AdmissionControl) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only gate state-mutating requests (POST reservations, payments, confirms)
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// Try direct acquire
		select {
		case ac.semaphore <- struct{}{}:
			// Acquired immediately
			ac.mu.Lock()
			ac.activeCount++
			ac.mu.Unlock()

			defer func() {
				<-ac.semaphore
				ac.mu.Lock()
				ac.activeCount--
				ac.mu.Unlock()
			}()

			c.Next()
			return

		default:
			// Capacity reached: try to enter the waiting queue
			ac.mu.Lock()
			if ac.waitingCount >= ac.queueCapacity {
				ac.mu.Unlock()
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error":       "CAPACITY_EXCEEDED",
					"message":     "System is currently experiencing high load. Please retry in a few moments.",
					"status_code": http.StatusServiceUnavailable,
				})
				return
			}
			ac.waitingCount++
			ac.mu.Unlock()

			waitChan := make(chan bool, 1)

			select {
			case ac.waitingQueue <- waitChan:
				// Successfully queued, wait for dispatcher slot or timeout
				ctx, cancel := context.WithTimeout(c.Request.Context(), ac.queueTimeout)
				defer cancel()

				select {
				case <-waitChan:
					// Slot granted!
					ac.mu.Lock()
					ac.waitingCount--
					ac.activeCount++
					ac.mu.Unlock()

					defer func() {
						<-ac.semaphore
						ac.mu.Lock()
						ac.activeCount--
						ac.mu.Unlock()
					}()

					c.Next()
					return

				case <-ctx.Done():
					// Queue wait timed out
					ac.mu.Lock()
					ac.waitingCount--
					ac.mu.Unlock()

					c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"error":       "QUEUE_TIMEOUT",
						"message":     "Request wait time in queue exceeded limit. Please try again.",
						"status_code": http.StatusTooManyRequests,
					})
					return
				}

			default:
				// Queue channel full
				ac.mu.Lock()
				ac.waitingCount--
				ac.mu.Unlock()

				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error":       "ADMISSION_QUEUE_FULL",
					"message":     "High traffic volume, reservation queue is full.",
					"status_code": http.StatusServiceUnavailable,
				})
				return
			}
		}
	}
}
