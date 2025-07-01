package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis address
		Password: "",               // No password set
		DB:       0,                // Default DB
	})

	// Ping Redis to check connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	fmt.Println("Connected to Redis. Waiting for jobs...")

	for {
		// BLPOP command to pop a job from the 'bull:submissions:wait' list
		// This is how BullMQ stores jobs before they are processed.
		// The key might vary slightly depending on BullMQ version and configuration,
		// but 'bull:queueName:wait' is a common pattern for unprocessed jobs.
		// We'll use 'bull:submissions:wait' based on typical BullMQ internal key naming.
		// If jobs are not consumed, you might need to check BullMQ's actual internal keys
		// or use BullMQ's dedicated Go client if available (though go-redis is sufficient for raw commands).
		result, err := rdb.BLPop(ctx, 0, "bull:submissions:wait").Result()
		if err != nil {
			log.Printf("Error receiving from Redis: %v", err)
			time.Sleep(1 * time.Second) // Prevent busy-loop on error
			continue
		}

		// result[0] is the key, result[1] is the value (the job data)
		if len(result) < 2 {
			log.Printf("Received malformed message: %v", result)
			continue
		}

		jobData := result[1]
		fmt.Printf("Received raw job data: %s", jobData)

		// In a real scenario, you'd parse jobData (which is JSON)
		// and extract info like submissionId, code path, etc.
		// For now, we just print the raw message.
		fmt.Printf("Processing job from queue. Message: %s", jobData)

		// Simulate judge logic (no Docker here in the cloud environment)
		fmt.Println("Simulating judge process for this job...")
		// Placeholder for actual Docker execution and result comparison
		// cmd := exec.Command("docker", "run", ...)
		fmt.Println("Judge process finished for this job (simulated).")

		// In a real system, you would acknowledge the job and potentially add it
		// to a "completed" queue or update its status in the database.
		// BullMQ handles this complex state management internally.
		// For this simple consumer, we just process and continue.
	}
}
