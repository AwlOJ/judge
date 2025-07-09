package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"judge-service/internal/queue"
	"judge-service/internal/runner"
	"judge-service/internal/store"

	"errors"
	"judge-service/internal/core"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found or error loading .env")
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Configuration (will be moved to internal/config later) ---
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Fatal("REDIS_URL environment variable not set")
	}

	queueName := os.Getenv("REDIS_QUEUE_NAME")
	if queueName == "" {
		queueName = "submission_queue"
	}

	// Get MongoDB configuration from environment variables
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI environment variable not set")
	}
	mongoDBName := os.Getenv("MONGO_DB_NAME")
	if mongoDBName == "" {
		log.Fatal("MONGO_DB_NAME environment variable not set")
	}

	// --- Connect to Redis ---
	log.Printf("Connecting to Redis using URL: %s", redisURL)
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("Error parsing Redis URL: %v", err)
	}
	rdb := redis.NewClient(opt)

	// Ping Redis to check connection
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	log.Println("Successfully connected to Redis.")

	// --- Connect to MongoDB ---
	log.Printf("Connecting to MongoDB...")
	storeInstance, err := store.NewStore(ctx, mongoURI, mongoDBName)
	if err != nil {
		log.Fatalf("Could not connect to MongoDB: %v", err)
	}
	defer storeInstance.Close(ctx) // Ensure MongoDB connection is closed on exit

	log.Println("Successfully connected to MongoDB.")

	// --- Create Runner ---
	runnerInstance := runner.NewRunner()

	// --- Create and Start Queue Consumer ---
	consumer := queue.NewConsumer(rdb, queueName)

	// Define the job handler function
	jobHandler := func(ctx context.Context, job *queue.JobPayload) error {
		log.Printf("Handling job for submission ID: %s", job.SubmissionID)

		// Defer cleanup of the temporary environment immediately
		var tempDir string
		defer func() {
			if tempDir != "" {
				if cleanupErr := runnerInstance.CleanupEnvironment(tempDir); cleanupErr != nil {
					log.Printf("Error cleaning up temp directory %s: %v", tempDir, cleanupErr)
				}
			}
		}()

		// Initial status update - Judging
		updateStatus := func(status string) {
			if updateErr := storeInstance.UpdateSubmissionStatus(ctx, job.SubmissionID, status); updateErr != nil {
				log.Printf("Failed to update submission %s status to %s: %v", job.SubmissionID, status, updateErr)
			}
		}
		updateResult := func(status string, execTimeMs, memoryUsedKb int64) {
			if updateErr := storeInstance.UpdateSubmissionResult(ctx, job.SubmissionID, status, execTimeMs, memoryUsedKb); updateErr != nil {
				log.Printf("Failed to update submission %s result to %s: %v", job.SubmissionID, status, updateErr)
			}
		}

		updateStatus("Judging")

		// --- Actual Judging Logic Starts Here ---

		// 1. Fetching data from MongoDB
		submission, err := storeInstance.FetchSubmission(ctx, job.SubmissionID)
		if err != nil {
			log.Printf("Error fetching submission %s: %v", job.SubmissionID, err)
			updateStatus("Internal Error") // Update status on fetch error
			return nil                     // Do not retry this job if data fetching failed
		}
		log.Printf("Fetched submission for problem ID: %s", submission.ProblemID.Hex())

		problem, err := storeInstance.FetchProblem(ctx, submission.ProblemID.Hex())
		if err != nil {
			log.Printf("Error fetching problem %s for submission %s: %v", submission.ProblemID.Hex(), job.SubmissionID, err)
			updateStatus("Internal Error") // Update status on fetch error
			return nil                     // Do not retry this job
		}
		log.Printf("Fetched problem: %s", problem.Title)

		// 2. Preparing the environment
		tempDir, err = runnerInstance.PrepareEnvironment(job.SubmissionID, submission.Code, problem.TestCases, submission.Language)
		if err != nil {
			log.Printf("Error preparing environment for submission %s: %v", job.SubmissionID, err)
			updateStatus("Internal Error")
			return nil // Do not retry
		}
		log.Printf("Environment prepared in %s", tempDir)

		// 3. Compile the code
		executablePath, compileErr := runnerInstance.Compile(ctx, job.SubmissionID, submission.Language, tempDir)
		if compileErr != nil {
			log.Printf("Compilation failed for submission %s: %v", job.SubmissionID, compileErr)
			updateResult("Compilation Error", 0, 0) // Update with 0 time/memory
			return nil                              // Do not retry on compilation errors, just report and finish
		}
		log.Printf("Compilation successful. Executable: %s", executablePath)

		// 4. Execute against test cases and Judge
		var finalStatus = "Accepted"
		var totalExecTimeMs int64 = 0
		var maxMemoryUsedKb int64 = 0
		var numofloops int64 = 0

		for i, testCase := range problem.TestCases {
			log.Printf("Running test case %d for submission %s...", i, job.SubmissionID)

			output, executionTimeMs, memoryUsedKb, runtimeErr := runnerInstance.Execute(
				ctx, job.SubmissionID, submission.Language, executablePath, &testCase,
				problem.TimeLimit, problem.MemoryLimit, tempDir,
			)

			// Aggregate total execution time and max memory used
			totalExecTimeMs += int64(executionTimeMs)
			numofloops++
			if int64(memoryUsedKb) > maxMemoryUsedKb {
				maxMemoryUsedKb = int64(memoryUsedKb)
			}

			if runtimeErr != nil {
				// Check for specific runtime error types
				if errors.Is(runtimeErr, context.DeadlineExceeded) || strings.Contains(runtimeErr.Error(), "Time Limit Exceeded") {
					finalStatus = "Time Limit Exceeded"
				} else if strings.Contains(runtimeErr.Error(), "Memory Limit Exceeded") {
					finalStatus = "Memory Limit Exceeded"
				} else {
					finalStatus = "Runtime Error"
				}
				log.Printf("Submission %s - Test case %d: %s (Error: %v)", job.SubmissionID, i, finalStatus, runtimeErr)
				break // Stop on first error
			}

			// Compare output using internal/core/engine
			if core.CompareOutputs(output, testCase.Output) {
				log.Printf("Submission %s - Test case %d: Accepted", job.SubmissionID, i)
			} else {
				log.Printf("Submission %s - Test case %d: Wrong Answer", job.SubmissionID, i)
				finalStatus = "Wrong Answer"
				break // Stop on first wrong answer
			}
		}

		totalExecTimeMs /= numofloops

		// 5. Updating submission status/result in MongoDB
		log.Printf("Finalizing submission %s with status: %s, Time: %dms, Memory: %dKB", job.SubmissionID, finalStatus, totalExecTimeMs, maxMemoryUsedKb)
		updateResult(finalStatus, totalExecTimeMs, maxMemoryUsedKb)

		// --- End of Judging Logic ---

		log.Printf("Finished handling job for submission ID: %s", job.SubmissionID)
		return nil // Indicate successful processing
	}

	// Use a goroutine to start the consumer so main can listen for signals
	go consumer.Start(ctx, jobHandler)

	// --- Graceful Shutdown ---
	// Listen for OS signals (like Ctrl+C or termination signals)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	// Block until a signal is received
	<-stopChan
	log.Println("Shutdown signal received, waiting for ongoing jobs...")

	// Cancel the context to signal the consumer to stop
	cancel()

	// In a real-world scenario with long-running jobs, you might want to wait
	// for the consumer goroutine to finish gracefully here.
	// For simplicity now, we just rely on context cancellation.

	log.Println("Judge daemon stopped.")
}
