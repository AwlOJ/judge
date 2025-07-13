package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"judge-service/internal/core"
	"judge-service/internal/queue"
	"judge-service/internal/runner"
	"judge-service/internal/store"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func init() {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading .env file")
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Graceful Shutdown Setup ---
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	// --- Configuration ---
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Fatal("REDIS_URL environment variable not set")
	}
	queueName := os.Getenv("REDIS_QUEUE_NAME")
	if queueName == "" {
		queueName = "submission_queue"
	}
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI environment variable not set")
	}
	mongoDBName := os.Getenv("MONGO_DB_NAME")
	if mongoDBName == "" {
		log.Fatal("MONGO_DB_NAME environment variable not set")
	}

	// --- Connect to Redis ---
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("Error parsing Redis URL: %v", err)
	}
	rdb := redis.NewClient(opt)
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	log.Println("Successfully connected to Redis.")

	// --- Connect to MongoDB ---
	storeInstance, err := store.NewStore(ctx, mongoURI, mongoDBName)
	if err != nil {
		log.Fatalf("Could not connect to MongoDB: %v", err)
	}
	defer func() {
		if err := storeInstance.Close(context.Background()); err != nil {
			log.Printf("Error closing MongoDB connection: %v", err)
		}
	}()
	log.Println("Successfully connected to MongoDB.")

	// --- Create Runner ---
	runnerInstance, err := runner.NewRunner()
	if err != nil {
		log.Fatalf("Failed to create runner: %v", err)
	}

	// --- Create and Start Queue Consumer ---
	consumer := queue.NewConsumer(rdb, queueName)

	// Define the job handler function
	jobHandler := func(ctx context.Context, job *queue.JobPayload) error {
		return processJob(ctx, job, storeInstance, runnerInstance)
	}

	// Start the consumer in a goroutine
	go consumer.Start(ctx, jobHandler)

	// --- Wait for shutdown signal ---
	<-stopChan
	log.Println("Shutdown signal received, gracefully stopping...")
	cancel() // Trigger context cancellation

	// Give some time for the consumer to stop cleanly.
	time.Sleep(2 * time.Second)

	log.Println("Judge daemon stopped.")
}

func processJob(ctx context.Context, job *queue.JobPayload, storeInstance *store.Store, runnerInstance *runner.Runner) error {
	log.Printf("Handling job for submission ID: %s", job.SubmissionID)
	
	var tempDir string // Define tempDir here to make it accessible for deferred cleanup
	
	// Use a named function for deferred cleanup to avoid capturing loop variables.
	defer func() {
		if tempDir != "" {
			if cleanupErr := runnerInstance.CleanupEnvironment(tempDir); cleanupErr != nil {
				log.Printf("Error cleaning up temp directory %s: %v", tempDir, cleanupErr)
			}
		}
	}()

	updateStatus := func(status string) {
		if err := storeInstance.UpdateSubmissionStatus(ctx, job.SubmissionID, status); err != nil {
			log.Printf("Failed to update submission %s status to %s: %v", job.SubmissionID, status, err)
		}
	}
	updateResult := func(status string, execTimeMs, memoryUsedKb int64) {
		if err := storeInstance.UpdateSubmissionResult(ctx, job.SubmissionID, status, execTimeMs, memoryUsedKb); err != nil {
			log.Printf("Failed to update submission %s result to %s: %v", job.SubmissionID, status, err)
		}
	}

	updateStatus("Judging")

	// 1. Fetch data from MongoDB
	submission, err := storeInstance.FetchSubmission(ctx, job.SubmissionID)
	if err != nil {
		log.Printf("Error fetching submission %s: %v", job.SubmissionID, err)
		updateStatus("Internal Error")
		return nil // Acknowledge the job, don't retry if data is missing
	}

	problem, err := storeInstance.FetchProblem(ctx, submission.ProblemID.Hex())
	if err != nil {
		log.Printf("Error fetching problem %s for submission %s: %v", submission.ProblemID.Hex(), job.SubmissionID, err)
		updateStatus("Internal Error")
		return nil // Acknowledge the job
	}

	// 2. Prepare the environment
	tempDir, err = runnerInstance.PrepareEnvironment(job.SubmissionID, submission.Code, submission.Language)
	if err != nil {
		log.Printf("Error preparing environment for submission %s: %v", job.SubmissionID, err)
		updateStatus("Internal Error")
		return nil // Acknowledge the job
	}

	// 3. Compile the code
	executablePath, compileErr := runnerInstance.Compile(ctx, job.SubmissionID, submission.Language, tempDir)
	if compileErr != nil {
		log.Printf("Compilation failed for submission %s: %v", job.SubmissionID, compileErr)
		updateResult("Compilation Error", 0, 0)
		return nil // Acknowledge the job
	}

	// 4. Execute against test cases
	var finalStatus = "Accepted"
	var totalExecTimeMs int64 = 0
	var maxMemoryUsedKb int64 = 0
	
	for i, testCase := range problem.TestCases {
		log.Printf("Running test case %d for submission %s...", i+1, job.SubmissionID)
		
		output, execTimeMs, memoryUsedKb, runtimeErr := runnerInstance.Execute(
			ctx, job.SubmissionID, submission.Language, executablePath, &testCase,
			problem.TimeLimit, problem.MemoryLimit,
		)
		
		totalExecTimeMs += int64(execTimeMs)
		if int64(memoryUsedKb) > maxMemoryUsedKb {
			maxMemoryUsedKb = int64(memoryUsedKb)
		}

		if runtimeErr != nil {
			finalStatus = "Runtime Error" // Default
			errMsg := runtimeErr.Error()
			if strings.Contains(errMsg, "Time Limit Exceeded") {
				finalStatus = "Time Limit Exceeded"
			} else if strings.Contains(errMsg, "Memory Limit Exceeded") {
				finalStatus = "Memory Limit Exceeded"
			}
			// THIS IS THE CRITICAL CHANGE - LOG THE ACTUAL ERROR
			log.Printf("Submission %s - Test case %d failed with status: %s. Reason: %v", job.SubmissionID, i+1, finalStatus, runtimeErr)
			updateResult(finalStatus, int64(execTimeMs), maxMemoryUsedKb)
			return nil // Stop processing and return
		}
		
		if !core.CompareOutputs(output, testCase.Output) {
			log.Printf("Submission %s - Test case %d: Wrong Answer", job.SubmissionID, i+1)
			finalStatus = "Wrong Answer"
			updateResult(finalStatus, totalExecTimeMs / int64(i+1), maxMemoryUsedKb)
			return nil // Stop processing and return
		}

		log.Printf("Submission %s - Test case %d: Passed", job.SubmissionID, i+1)
	}

	avgExecTimeMs := totalExecTimeMs / int64(len(problem.TestCases))
	
	// 5. Update final result
	log.Printf("Finalizing submission %s with status: %s, Avg Time: %dms, Max Memory: %dKB", job.SubmissionID, finalStatus, avgExecTimeMs, maxMemoryUsedKb)
	updateResult(finalStatus, avgExecTimeMs, maxMemoryUsedKb)

	return nil
}
