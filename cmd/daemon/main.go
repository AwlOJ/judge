package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"judge-service/internal/queue"
	"judge-service/internal/runner"
	"judge-service/internal/store"

	"github.com/redis/go-redis/v9"
	"judge-service/internal/core"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Configuration (will be moved to internal/config later) ---
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
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
	log.Printf("Connecting to Redis at %s", redisAddr)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"), // Load password from ENV
		DB:       0,                           // Default DB
	})

	// Ping Redis to check connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	log.Println("Successfully connected to Redis.")

	// --- Connect to MongoDB ---
	log.Printf("Connecting to MongoDB...")
	store, err := store.NewStore(ctx, mongoURI, mongoDBName)
	if err != nil {
		log.Fatalf("Could not connect to MongoDB: %v", err)
	}
	defer store.Close(ctx) // Ensure MongoDB connection is closed on exit

	log.Println("Successfully connected to MongoDB.")

	// --- Create Runner ---
	runner := runner.NewRunner()

	// --- Create and Start Queue Consumer ---
	consumer := queue.NewConsumer(rdb, queueName)

	// Define the job handler function
	jobHandler := func(ctx context.Context, job *queue.JobPayload) error {
		log.Printf("Handling job for submission ID: %s", job.SubmissionID)

		// --- Actual Judging Logic Starts Here ---

		// 1. Fetching data from MongoDB
		submission, err := store.FetchSubmission(ctx, job.SubmissionID)
		if err != nil {
			log.Printf("Error fetching submission %s: %v", job.SubmissionID, err)
			// TODO: Update submission status to indicate error
			return err // Propagate error so consumer can potentially handle retries
		}
		log.Printf("Fetched submission for problem ID: %s", submission.ProblemID.Hex())

		problem, err := store.FetchProblem(ctx, submission.ProblemID.Hex())
		if err != nil {
			log.Printf("Error fetching problem %s for submission %s: %v", submission.ProblemID.Hex(), job.SubmissionID, err)
			// TODO: Update submission status to indicate error
			return err // Propagate error
		}
		log.Printf("Fetched problem: %s", problem.Title)

		// Defer cleanup of the temporary environment
		var tempDir string
		defer func() {
			if tempDir != "" {
				if cleanupErr := runner.CleanupEnvironment(tempDir); cleanupErr != nil {
					log.Printf("Error cleaning up temp directory %s: %v", tempDir, cleanupErr)
				}
			}
		}()

		// 2. Preparing the environment
		tempDir, err = runner.PrepareEnvironment(job.SubmissionID, submission.Code, problem.TestCases)
		if err != nil {
			log.Printf("Error preparing environment for submission %s: %v", job.SubmissionID, err)
			// TODO: Update submission status to indicate error
			return err // Propagate error
		}
		log.Printf("Environment prepared in %s", tempDir)

		// 3. Compile the code
		executablePath, compileErr := runner.Compile(ctx, job.SubmissionID, submission.Language, tempDir)
		if compileErr != nil {
			log.Printf("Compilation failed for submission %s: %v", job.SubmissionID, compileErr)
			// TODO: Update submission status to Compilation Error
			return nil // Do not retry on compilation errors, just report and finish
		}
		log.Printf("Compilation successful. Executable: %s", executablePath)

		// 4. Execute against test cases and Judge (placeholder for internal/core)
		// This loop will be replaced by a call to the core judging engine later
		log.Println("Executing test cases (placeholder)...")
		for i, testCase := range problem.TestCases {
			log.Printf("Running test case %d...", i)
			// Simulate execution and get results
			output, _, _, runtimeErr := runner.Execute(ctx, job.SubmissionID, submission.Language, executablePath, &testCase, problem.TimeLimit, problem.MemoryLimit, tempDir)
			if runtimeErr != nil {
				log.Printf("Runtime error on test case %d for submission %s: %v", i, job.SubmissionID, runtimeErr)
				// TODO: Update submission status to Runtime Error and break loop
				break
			}
				// Compare output using internal/core/engine
				if core.CompareOutputs(output, testCase.Output) {
					log.Printf("Test case %d for submission %s: Accepted", i, job.SubmissionID)
					// TODO: Update submission result to Accepted
				} else {
					log.Printf("Test case %d for submission %s: Wrong Answer", i, job.SubmissionID)
					// TODO: Update submission result to Wrong Answer and break the loop
					break
				}
			}


			// 5. Updating submission status/result in MongoDB (using internal/store)
			// This will be done after the actual judging logic determines the final verdict

		// --- End of Judging Logic --- 

		log.Printf("Finished handling job for submission ID: %s", job.SubmissionID)
		return nil // Indicate successful processing for now
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