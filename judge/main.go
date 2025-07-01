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
		// BLPOP command to pop a job from the 'simple-judge-queue' list
		result, err := rdb.BLPop(ctx, 0, "simple-judge-queue").Result()
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
