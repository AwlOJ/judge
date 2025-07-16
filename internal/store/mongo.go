package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Status Constants ---
const (
	StatusPending             = "Pending"
	StatusJudging             = "Judging"
	StatusAccepted            = "Accepted"
	StatusWrongAnswer         = "Wrong Answer"
	StatusTimeLimitExceeded   = "Time Limit Exceeded"
	StatusMemoryLimitExceeded = "Memory Limit Exceeded"
	StatusCompilationError    = "Compilation Error"
	StatusRuntimeError        = "Runtime Error"
	StatusInternalError       = "Internal Error"
	StatusCompleted           = "Completed" // Internal status for a successful run before comparison
)

// --- Data Structures ---

// TestCase defines the structure for a single test case.
type TestCase struct {
	Input  string `bson:"input"`
	Output string `bson:"output"`
}

// Problem holds all information about a single competitive programming problem.
type Problem struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	Title         string             `bson:"title"`
	Description   string             `bson:"description"`
	TimeLimitMs   int                `bson:"timeLimitMs"`
	MemoryLimitMb int                `bson:"memoryLimitMb"`
	TestCases     []TestCase         `bson:"testCases"`
}

// Submission represents a user's code submission for a problem.
type Submission struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	ProblemID  primitive.ObjectID `bson:"problemId"`
	SourceCode string             `bson:"sourceCode"`
	Language   string             `bson:"language"`
	Status     string             `bson:"status"`
}

// --- Payloads and Results ---

// SubmissionPayload is the message sent to the queue.
type SubmissionPayload struct {
	SubmissionID string `json:"submissionId"`
}

// ExecutionResult is the raw result from running the code against one test case.
type ExecutionResult struct {
	Status          string
	Error           string
	Output          string
	ExecutionTimeMs int
	MemoryUsedKb    uint64
}

// SubmissionResult is the final result to be stored in the database.
type SubmissionResult struct {
	Status          string `bson:"status"`
	ExecutionTimeMs int    `bson:"executionTimeMs,omitempty"`
	MemoryUsedKb    uint64 `bson:"memoryUsedKb,omitempty"`
	CompileOutput   string `bson:"compileOutput,omitempty"`
}


// MongoStore holds the database connection.
type MongoStore struct {
	client *mongo.Client
	db     *mongo.Database
}

// NewMongoStore creates and returns a new MongoStore.
func NewMongoStore(ctx context.Context, uri, dbName string) (*MongoStore, error) {
	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mongo: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping mongo: %w", err)
	}

	db := client.Database(dbName)
	return &MongoStore{client: client, db: db}, nil
}

// Close disconnects from MongoDB.
func (s *MongoStore) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// GetSubmission retrieves a submission by its ID.
func (s *MongoStore) GetSubmission(ctx context.Context, id string) (*Submission, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid submission ID format: %w", err)
	}

	var submission Submission
	err = s.db.Collection("submissions").FindOne(ctx, bson.M{"_id": objID}).Decode(&submission)
	if err != nil {
		return nil, err
	}
	return &submission, nil
}

// GetProblem retrieves a problem by its ID.
func (s *MongoStore) GetProblem(ctx context.Context, id primitive.ObjectID) (*Problem, error) {
	var problem Problem
	err := s.db.Collection("problems").FindOne(ctx, bson.M{"_id": id}).Decode(&problem)
	if err != nil {
		return nil, err
	}
	return &problem, nil
}

// UpdateSubmissionStatus updates only the status of a submission.
func (s *MongoStore) UpdateSubmissionStatus(ctx context.Context, id, status string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid submission ID format: %w", err)
	}
	_, err = s.db.Collection("submissions").UpdateOne(
		ctx,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{"status": status, "updatedAt": time.Now()}},
	)
	return err
}

// UpdateSubmissionResult updates the submission with the final result.
func (s *MongoStore) UpdateSubmissionResult(ctx context.Context, id string, result SubmissionResult) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid submission ID format: %w", err)
	}
	_, err = s.db.Collection("submissions").UpdateOne(
		ctx,
		bson.M{"_id": objID},
		bson.M{"$set": result},
	)
	return err
}
