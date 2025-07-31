package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"judge-service/internal/callback"
	"judge-service/internal/config"
	"judge-service/internal/core"
	"judge-service/internal/queue"
	"judge-service/internal/runner"
	"judge-service/internal/store"

	"github.com/joho/godotenv"
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading .env file")
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize the new callback client
	callbackClient := callback.NewClient(cfg.InternalApiUrl, cfg.InternalApiSecret)

	consumer, err := queue.NewConsumer(cfg.RedisURL, cfg.RedisQueueName)
	if err != nil {
		log.Fatalf("Could not initialize queue consumer: %v", err)
	}
	log.Println("Successfully connected to Redis.")

	storeInstance, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDBName)
	if err != nil {
		log.Fatalf("Could not connect to MongoDB: %v", err)
	}
	defer func() {
		if err := storeInstance.Close(context.Background()); err != nil {
			log.Printf("Error closing MongoDB connection: %v", err)
		}
	}()
	log.Println("Successfully connected to MongoDB.")

	langConfig, err := config.LoadLanguageConfig()
	if err != nil {
		log.Fatalf("Failed to load language configurations: %v", err)
	}
	runnerInstance := runner.NewRunner(ctx, langConfig)

	jobHandler := func(ctx context.Context, payload *store.SubmissionPayload) error {
		// Pass the callback client to the job processor
		return processJob(ctx, payload, storeInstance, runnerInstance, callbackClient)
	}

	go consumer.Start(ctx, jobHandler)

	<-stopChan
	log.Println("Shutdown signal received, gracefully stopping...")
	cancel()

	time.Sleep(2 * time.Second)
	log.Println("Judge daemon stopped.")
}

func processJob(ctx context.Context, payload *store.SubmissionPayload, s *store.MongoStore, r *runner.Runner, cb *callback.Client) error {
	log.Printf("Processing submission ID: %s", payload.SubmissionID)

	var tempDir string
	defer func() {
		if tempDir != "" {
			r.CleanUp(tempDir)
		}
	}()

	// The judge service still updates the status to "Judging"
	err := s.UpdateSubmissionStatus(ctx, payload.SubmissionID, store.StatusJudging)
	if err != nil {
		log.Printf("Failed to update submission %s status to Judging: %v", payload.SubmissionID, err)
	}

	submission, err := s.GetSubmission(ctx, payload.SubmissionID)
	if err != nil {
		log.Printf("Error fetching submission %s: %v", payload.SubmissionID, err)
		// No need to update status here, let the API server handle it if it times out
		return err
	}

	problem, err := s.GetProblem(ctx, submission.ProblemID)
	if err != nil {
		log.Printf("Error fetching problem %s for submission %s: %v", submission.ProblemID, payload.SubmissionID, err)
		return err
	}

	tempDir, err = r.PrepareEnvironment(payload.SubmissionID, submission.Code, submission.Language)
	if err != nil {
		log.Printf("Error preparing environment for %s: %v", payload.SubmissionID, err)
		result := store.SubmissionResult{Status: store.StatusInternalError}
		return cb.SendResult(payload.SubmissionID, result)
	}

	executablePath, compileOutput, err := r.Compile(tempDir, submission.Language)
	if err != nil {
		log.Printf("Compilation failed for %s. Compiler output: %s", payload.SubmissionID, compileOutput)
		result := store.SubmissionResult{
			Status:        store.StatusCompilationError,
			CompileOutput: compileOutput,
		}
		return cb.SendResult(payload.SubmissionID, result)
	}

	var finalStatus = store.StatusAccepted
	var totalExecTimeMs int
	var maxMemoryUsedKb uint64

	for i, testCase := range problem.TestCases {
		log.Printf("Running test case %d for submission %s...", i+1, payload.SubmissionID)
		
		timeLimitMs := problem.TimeLimit * 1000
		execResult := r.Execute(executablePath, testCase, timeLimitMs, problem.MemoryLimit)

		if execResult.MemoryUsedKb > maxMemoryUsedKb {
			maxMemoryUsedKb = execResult.MemoryUsedKb
		}
		totalExecTimeMs += execResult.ExecutionTimeMs

		if execResult.Status != store.StatusCompleted {
			finalStatus = execResult.Status
			log.Printf("Submission %s - Test case %d failed with status: %s", payload.SubmissionID, i+1, finalStatus)
			result := store.SubmissionResult{
				Status:        finalStatus,
				ExecutionTime: execResult.ExecutionTimeMs,
				MemoryUsed:    execResult.MemoryUsedKb,
			}
			return cb.SendResult(payload.SubmissionID, result)
		}

		if !core.CompareOutputs(execResult.Output, testCase.Output) {
			finalStatus = store.StatusWrongAnswer
			log.Printf("Submission %s - Test case %d: Wrong Answer", payload.SubmissionID, i+1)
			result := store.SubmissionResult{
				Status:        finalStatus,
				ExecutionTime: execResult.ExecutionTimeMs,
				MemoryUsed:    maxMemoryUsedKb,
			}
			return cb.SendResult(payload.SubmissionID, result)
		}
		log.Printf("Submission %s - Test case %d: Passed", payload.SubmissionID, i+1)
	}

	var avgExecTimeMs int
	if len(problem.TestCases) > 0 {
		avgExecTimeMs = totalExecTimeMs / len(problem.TestCases)
	}

	finalResult := store.SubmissionResult{
		Status:        finalStatus,
		ExecutionTime: avgExecTimeMs,
		MemoryUsed:    maxMemoryUsedKb,
	}
	log.Printf("Finalizing submission %s with status: %s. Sending to callback.", payload.SubmissionID, finalStatus)
	return cb.SendResult(payload.SubmissionID, finalResult)
}
