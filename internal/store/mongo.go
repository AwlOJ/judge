package store

import (
	"context"
	"fmt"
	"log"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Submission represents a user's code submission.
// This struct should match the 'submissions' collection schema.
type Submission struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	UserID    primitive.ObjectID `bson:"userId"`
	ProblemID primitive.ObjectID `bson:"problemId"`
	Code      string             `bson:"code"`
	Language  string             `bson:"language"`
	Status    string             `bson:"status"` // e.g., "In Queue", "Judging", "Accepted", "Wrong Answer"
	// Add other fields like executionTime, memoryUsed later
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
	// Add other problem details later
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
	log.Printf("Fetching submission with ID: %s (placeholder)", submissionID)
	// --- Implement actual MongoDB query here ---
	// For now, return a dummy submission or an error
	return nil, fmt.Errorf("FetchSubmission not implemented yet")
}

// FetchProblem retrieves a problem document by its ID.
// We will implement the actual database query here later.
func (s *Store) FetchProblem(ctx context.Context, problemID string) (*Problem, error) {
	log.Printf("Fetching problem with ID: %s (placeholder)", problemID)
	// --- Implement actual MongoDB query here ---
	// For now, return a dummy problem or an error
	return nil, fmt.Errorf("FetchProblem not implemented yet")
}
