package queue

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"judge-service/internal/store"
	"github.com/redis/go-redis/v9"
)

// Consumer is responsible for listening to the Redis queue.
type Consumer struct {
	RDB       *redis.Client
	QueueName string
}

// NewConsumer creates a new queue consumer and pings the Redis server.
func NewConsumer(redisURL string, queueName string) (*Consumer, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)

	// Ping the server to ensure connection is alive
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		return nil, err
	}

	return &Consumer{
		RDB:       rdb,
		QueueName: queueName,
	}, nil
}

// Start begins listening for jobs on the configured Redis queue.
func (c *Consumer) Start(ctx context.Context, handler func(context.Context, *store.SubmissionPayload) error) {
	log.Printf("[*] Waiting for jobs on queue %s", c.QueueName)

	for {
		select {
		case <-ctx.Done():
			log.Println("Consumer context done. Stopping.")
			return
		default:
			// Pop a job from the list, with a 0 timeout for blocking
			result, err := c.RDB.BLPop(ctx, 0, c.QueueName).Result()
			if err != nil {
				if err == context.Canceled || err == redis.Nil {
					return // Normal exit condition
				}
				log.Printf("Error receiving from Redis: %v", err)
				time.Sleep(1 * time.Second) // Prevent busy-looping on other errors
				continue
			}

			if len(result) < 2 {
				continue
			}

			jobDataString := result[1]
			log.Printf("Received job data: %s", jobDataString)

			var payload store.SubmissionPayload
			if err := json.Unmarshal([]byte(jobDataString), &payload); err != nil {
				log.Printf("Error unmarshalling job data %s: %v", jobDataString, err)
				continue
			}
			
			// A submission ID must be present
			if payload.SubmissionID == "" {
				log.Println("Received job with empty submission ID.")
				continue
			}

			if err := handler(ctx, &payload); err != nil {
				log.Printf("Error handling job for submission %s: %v", payload.SubmissionID, err)
				continue
			}

			log.Printf("Finished processing submission ID: %s", payload.SubmissionID)
		}
	}
}
