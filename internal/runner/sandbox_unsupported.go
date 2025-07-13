//go:build !linux

package runner

import (
	"context"
	"errors"
	"log"

	"judge-service/internal/store"
)

// Runner is a stub implementation for non-Linux environments.
// nsjail requires a Linux kernel, so code execution is not supported.
type Runner struct{}

// NewRunner returns an error on non-Linux systems.
func NewRunner() (*Runner, error) {
	return nil, errors.New("the nsjail runner is only supported on Linux")
}

// PrepareEnvironment is a stub.
func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, lang string) (tempDir string, err error) {
	log.Printf("Runner is not supported on this OS. Skipping PrepareEnvironment.")
	return "", errors.New("unsupported OS")
}

// Compile is a stub.
func (r *Runner) Compile(ctx context.Context, submissionID string, lang string, tempDir string) (string, error) {
	log.Printf("Runner is not supported on this OS. Skipping Compile.")
	return "", errors.New("unsupported OS")
}

// Execute is a stub.
func (r *Runner) Execute(ctx context.Context, submissionID, lang, executablePath string, testCase *store.TestCase, timeLimit int, memoryLimit int) (output string, execTimeMs int, memoryUsedKb int, runtimeErr error) {
	log.Printf("Runner is not supported on this OS. Skipping Execute.")
	return "", 0, 0, errors.New("unsupported OS")
}

// CleanupEnvironment is a stub.
func (r *Runner) CleanupEnvironment(tempDir string) error {
	log.Printf("Runner is not supported on this OS. Skipping CleanupEnvironment.")
	return nil
}
