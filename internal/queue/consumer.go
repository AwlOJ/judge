package queue

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// JobPayload represents the structure of a job in the queue.
// In our case, it contains the submission ID.
type JobPayload struct {
	SubmissionID string `json:"submissionId"`
}

// Consumer is responsible for listening to the Redis queue.
type Consumer struct {
	RDB       *redis.Client
	QueueName string
}

// NewConsumer creates a new queue consumer.
func NewConsumer(rdb *redis.Client, queueName string) *Consumer {
	return &Consumer{
		RDB:       rdb,
		QueueName: queueName,
	}
}

// Start begins listening for jobs on the configured Redis queue.
// It takes a job handler function that will be called for each received job.
func (c *Consumer) Start(ctx context.Context, handler func(context.Context, *JobPayload) error) {
	log.Printf("[*] Waiting for jobs on queue %s", c.QueueName)

	for {
		select {
		case <-ctx.Done():
			log.Println("Consumer context done. Stopping.")
			return // Exit loop if context is cancelled
		default:
			// BLPOP command to pop a job from the list, with a 0 timeout for blocking
			result, err := c.RDB.BLPop(ctx, 0, c.QueueName).Result()
			if err != nil {
				// Handle context cancellation specifically
				if err == context.Canceled {
					log.Println("Redis BLPOP cancelled.")
					return
				}
				log.Printf("Error receiving from Redis: %v", err)
				time.Sleep(1 * time.Second) // Prevent busy-loop on error
				continue
			}

			// result[0] is the key, result[1] is the value (the job data string)
			if len(result) < 2 {
				log.Printf("Received malformed message: %v", result)
				continue
			}

			jobDataString := result[1]
			log.Printf("Received job data: %s", jobDataString)

			// Parse the job data (assuming JSON payload like { "submissionId": "..." })
			var jobPayload JobPayload
			err = json.Unmarshal([]byte(jobDataString), &jobPayload)
			if err != nil {
				log.Printf("Error unmarshalling job data %s: %v", jobDataString, err)
				// In a real system, you might move this to a dead-letter queue
				continue
			}

			// Handle the job
			log.Printf("Processing submission ID: %s", jobPayload.SubmissionID)
			err = handler(ctx, &jobPayload)
			if err != nil {
				log.Printf("Error handling job for submission %s: %v", jobPayload.SubmissionID, err)
				// Depending on the error, you might retry or move to a dead-letter queue
				continue
			}

			log.Printf("Finished processing submission ID: %s", jobPayload.SubmissionID)
		}
	}
}
