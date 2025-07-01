package store

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Submission represents a user's code submission.
// This struct should match the 'submissions' collection schema.
type Submission struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	UserID        primitive.ObjectID `bson:"userId"`
	ProblemID     primitive.ObjectID `bson:"problemId"`
	Code          string             `bson:"code"`
	Language      string             `bson:"language"`
	Status        string             `bson:"status"` // e.g., "In Queue", "Judging", "Accepted", "Wrong Answer", "Compilation Error", "Runtime Error", "Time Limit Exceeded", "Memory Limit Exceeded", "Internal Error"
	ExecutionTime int64              `bson:"executionTime,omitempty"` // in milliseconds
	MemoryUsed    int64              `bson:"memoryUsed,omitempty"`    // in kilobytes
	CreatedAt     time.Time          `bson:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt"`
}

// Problem represents a problem definition.
// This struct should match the 'problems' collection schema.
type Problem struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	Title       string             `bson:"title"`
	Description string             `bson:"description"`
	TimeLimit   int                `bson:"timeLimit"`   // in seconds
	MemoryLimit int                `bson:"memoryLimit"` // in megabytes
	TestCases   []TestCase         `bson:"testCases"`
}

// TestCase represents a single test case for a problem.
type TestCase struct {
	Input  string `bson:"input"`
	Output string `bson:"output"`
}

// Store holds the MongoDB client instance and database name.
type Store struct {
	Client       *mongo.Client
	DatabaseName string
}

// NewStore creates a new Store instance and connects to MongoDB.
func NewStore(ctx context.Context, mongoURI string, dbName string) (*Store, error) {
	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Check the connection
	err = client.Ping(ctx, nil)
	if err != nil {
		// Attempt to disconnect if ping fails after connection
		go func() {
			if dErr := client.Disconnect(context.Background()); dErr != nil {
				log.Printf("Error during disconnect after failed ping: %v", dErr)
			}
		}()
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	log.Println("Connected to MongoDB successfully!")

	return &Store{Client: client, DatabaseName: dbName}, nil
}

// Close disconnects the MongoDB client.
func (s *Store) Close(ctx context.Context) error {
	log.Println("Closing MongoDB connection...")
	return s.Client.Disconnect(ctx)
}

// FetchSubmission retrieves a submission document by its ID.
func (s *Store) FetchSubmission(ctx context.Context, submissionID string) (*Submission, error) {
	objID, err := primitive.ObjectIDFromHex(submissionID)
	if err != nil {
		return nil, fmt.Errorf("invalid submission ID format: %w", err)
	}

	collection := s.Client.Database(s.DatabaseName).Collection("submissions")
	var submission Submission
	err = collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&submission)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("submission with ID %s not found", submissionID)
		}
		return nil, fmt.Errorf("failed to fetch submission %s: %w", submissionID, err)
	}
	return &submission, nil
}

// FetchProblem retrieves a problem document by its ID.
func (s *Store) FetchProblem(ctx context.Context, problemID string) (*Problem, error) {
	objID, err := primitive.ObjectIDFromHex(problemID)
	if err != nil {
		return nil, fmt.Errorf("invalid problem ID format: %w", err)
	}

	collection := s.Client.Database(s.DatabaseName).Collection("problems")
	var problem Problem
	err = collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&problem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("problem with ID %s not found", problemID)
		}
		return nil, fmt.Errorf("failed to fetch problem %s: %w", problemID, err)
	}
	return &problem, nil
}

// UpdateSubmissionStatus updates the status of a submission.
func (s *Store) UpdateSubmissionStatus(ctx context.Context, submissionID string, status string) error {
	objID, err := primitive.ObjectIDFromHex(submissionID)
	if err != nil {
		return fmt.Errorf("invalid submission ID format: %w", err)
	}

	collection := s.Client.Database(s.DatabaseName).Collection("submissions")
	filter := bson.M{"_id": objID}
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update status for submission %s: %w", submissionID, err)
	}
	log.Printf("Submission %s status updated to: %s", submissionID, status)
	return nil
}

// UpdateSubmissionResult updates the status, execution time, and memory usage of a submission.
func (s *Store) UpdateSubmissionResult(ctx context.Context, submissionID string, status string, execTimeMs int64, memoryUsedKb int64) error {
	objID, err := primitive.ObjectIDFromHex(submissionID)
	if err != nil {
		return fmt.Errorf("invalid submission ID format: %w", err)
	}

	collection := s.Client.Database(s.DatabaseName).Collection("submissions")
	filter := bson.M{"_id": objID}
	update := bson.M{
		"$set": bson.M{
			"status":        status,
			"executionTime": execTimeMs,
			"memoryUsed":    memoryUsedKb,
			"updatedAt":     time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update result for submission %s: %w", submissionID, err)
	}
	log.Printf("Submission %s result updated to: %s (Time: %dms, Memory: %dKB)", submissionID, status, execTimeMs, memoryUsedKb)
	return nil
}
