package runner

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"judge-service/internal/store"
	"errors"
)

// Runner handles the execution of code within Docker containers.
type Runner struct {
	// Configuration for language-specific Docker images and commands
	LangConfig map[string]struct {
		Image              string
		CompileCmd         []string
		RunCmd             []string
		SourceFileName     string
		ExecutableFileName string // For compiled languages
	}
}

// NewRunner creates a new Runner instance with predefined language configurations.
func NewRunner() *Runner {
	return &Runner{
		LangConfig: map[string]struct {
			Image              string
			CompileCmd         []string
			RunCmd             []string
			SourceFileName     string
			ExecutableFileName string
		}{
			"cpp": {
				Image:              "gcc:latest",
				CompileCmd:         []string{"g++", "/workspace/main.cpp", "-o", "/workspace/main.out", "-O2", "-static", "-Wall"},
				RunCmd:             []string{"/workspace/main.out"},
				SourceFileName:     "main.cpp",
				ExecutableFileName: "main.out",
			},
			"python": {
				Image:              "python:latest",
				CompileCmd:         nil, // Python is interpreted, no compile step
				RunCmd:             []string{"python3", "/workspace/main.py"},
				SourceFileName:     "main.py",
				ExecutableFileName: "", // Not applicable for interpreted languages
			},
			// Add configurations for other languages here
		},
	}
}

// Compile compiles the user's source code in a Docker container.
// It returns the path to the compiled executable inside the temp directory (relative to tempDir), or an error.
func (r *Runner) Compile(ctx context.Context, submissionID string, lang string, tempDir string) (executableRelativePath string, compileErr error) {
	log.Printf("Compiling submission %s (lang: %s) in %s", submissionID, lang, tempDir)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language for compilation: %s", lang)
	}

	// For interpreted languages (like Python), there is no explicit compile step.
	// We just return the source file path relative to the workspace.
	if len(config.CompileCmd) == 0 {
		log.Printf("Language %s is interpreted, no compilation needed.", lang)
		return config.SourceFileName, nil
	}

	// --- Build the Docker command for compilation ---
	// docker run --rm --network=none --cpus="1.0" --memory="512m" -v <tempDir>:/workspace <image> <compile_command...>
	args := []string{
		"run", "--rm", // Remove container after exit
		"--network=none",                            // No network access
		"--cpus=1.0",                                // Limit CPU usage
		"--memory=512m",                             // Limit memory usage (adjust as needed)
		"-v", fmt.Sprintf("%s:/workspace", tempDir), // Mount temp directory
		config.Image, // Docker image
	}

	// Append the actual compile command
	args = append(args, config.CompileCmd...)

	cmd := exec.CommandContext(ctx, "docker", args...)

	// Capture stderr for compilation errors
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	log.Printf("Running compile command: docker %s", strings.Join(args, " "))

	err := cmd.Run()

	// Check for compilation errors
	if err != nil {
		// Handle command execution error (e.g., docker not found, context cancelled)
		execErr := fmt.Errorf("docker compile command execution failed: %w (stderr: %s)", err, stderr.String())

		// Check if the error is due to the command exiting with a non-zero status
		if exitErr, ok := err.(*exec.ExitError); ok {
			execErr = fmt.Errorf("compilation failed with exit code %d (stderr: %s)", exitErr.ExitCode(), stderr.String())
		}

		log.Printf("Compilation failed for submission %s: %v", submissionID, execErr)
		return "", execErr // Return error indicating compilation failure
	}

	// Compilation successful
	log.Printf("Compilation successful for submission %s", submissionID)

	// Return the relative path to the compiled executable within the workspace
	// For interpreted languages, this will be the source file itself (handled above)
	// For compiled languages, it's the output file specified in LangConfig
	return config.ExecutableFileName, nil
}

// Execute runs the compiled code against a single test case in a Docker container.
// It returns the output, execution time, memory usage, and any runtime error.
func (r *Runner) Execute(ctx context.Context, submissionID string, lang string, executableRelativePath string, testCase *store.TestCase, timeLimit int, memoryLimit int, tempDir string) (output string, executionTimeMs int, memoryUsedMb int, runtimeErr error) {
	log.Printf("Executing submission %s (lang: %s) against test case (TimeLimit: %ds, MemoryLimit: %dMB)", submissionID, timeLimit, memoryLimit)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported language for execution: %s", lang)
	}

	// Define paths within the container's workspace
	workspaceInputPath := "/workspace/input.txt"
	workspaceOutputPath := "/workspace/output.txt" // Output file will be written here

	// Prepare the specific test case input file in the temp directory
	inputFilePath := filepath.Join(tempDir, "input.txt")
	if err := os.WriteFile(inputFilePath, []byte(testCase.Input), 0644); err != nil {
		return "", 0, 0, fmt.Errorf("failed to write test case input file to temp dir: %w", err)
	}
	// Ensure the temp input file is removed after execution
	defer os.Remove(inputFilePath)

	// Prepare the expected output file in the temp directory (optional, for easier debugging/comparison)
	// outputFilePath := filepath.Join(tempDir, "expected_output.txt")
	// if err := os.WriteFile(outputFilePath, []byte(testCase.Output), 0644); err != nil {
	// 	log.Printf("Warning: failed to write expected output file: %v", err)
	// }
	// defer os.Remove(outputFilePath)

	// --- Build the Docker command for execution ---
	// docker run --rm --network=none --cpus="1.0" --memory="512m" -v <tempDir>:/workspace <image> <run_command...> < /workspace/input.txt > /workspace/output.txt
	args := []string{
		"run", "--rm", // Remove container after exit
		"--network=none",                            // No network access
		fmt.Sprintf("--cpus=%f", float64(1.0)), // Limit CPU usage to 1 core
		fmt.Sprintf("--memory=%dm", memoryLimit), // Limit memory usage (in MB)
		"-v", fmt.Sprintf("%s:/workspace", tempDir), // Mount temp directory
		config.Image, // Docker image
	}

	// Append the actual run command, piping input and output within the container
	// The input is read from the mounted input.txt, and output is written to the mounted output.txt
	// Note: The exact command might vary slightly based on language/image, but this is a common pattern.
	runCmdWithRedirects := append(config.RunCmd, "<", workspaceInputPath, ">", workspaceOutputPath)
	args = append(args, runCmdWithRedirects...)

	// Add a timeout to the context for this execution
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeLimit)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "docker", args...)

	// Capture stderr (for runtime errors)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	log.Printf("Running execution command: docker %s", strings.Join(args, " "))

	// Record start time for rough execution time measurement
	startTime := time.Now()
	err := cmd.Run()
	executionTimeMs = int(time.Since(startTime).Milliseconds())

	// --- Handle Execution Results ---
	// Default status is successful execution
	runtimeErr = nil

	if err != nil {
		// Check if the error is due to timeout
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			log.Printf("Execution timed out for submission %s after %d seconds", submissionID, timeLimit)
			return "", executionTimeMs, 0, fmt.Errorf("Time Limit Exceeded")
		}

		// Handle other command execution errors or non-zero exit codes
		execErr := fmt.Errorf("docker run command execution failed: %w (stderr: %s)", err, stderr.String())

		if exitErr, ok := err.(*exec.ExitError); ok {
			// A non-zero exit code usually indicates a Runtime Error
			log.Printf("Execution failed with exit code %d for submission %s (stderr: %s)", exitErr.ExitCode(), submissionID, stderr.String())
			runtimeErr = fmt.Errorf("Runtime Error (Exit Code %d): %s", exitErr.ExitCode(), stderr.String())
			// Note: You might want to analyze stderr more for specific error types (e.g., Segmentation Fault, etc.)
		} else {
			// Other execution errors (e.g., docker daemon not running)
			log.Printf("Docker execution failed unexpectedly for submission %s: %v", submissionID, execErr)
			runtimeErr = fmt.Errorf("Execution System Error: %v", execErr)
		}
		// For now, we return empty output on error. You might decide to return stderr for RTE.
		return "", executionTimeMs, 0, runtimeErr
	}

	// Execution successful (exit code 0)
	log.Printf("Execution successful for submission %s", submissionID)

	// Read the output file generated by the program in the container
	outputFilePath := filepath.Join(tempDir, "output.txt")
	outputBytes, err := os.ReadFile(outputFilePath)
	// Ensure the temp output file is removed after reading
	defer os.Remove(outputFilePath)

	if err != nil {
		log.Printf("Error reading output file %s for submission %s: %v", outputFilePath, submissionID, err)
		return "", executionTimeMs, 0, fmt.Errorf("failed to read output file: %w", err)
	}

	// Convert output bytes to string
	output = string(outputBytes)

	// --- Memory Usage Measurement (Placeholder) ---
	// Measuring memory usage accurately inside Docker requires more advanced techniques
	// (e.g., reading cgroup stats from the host, using docker stats API if available and permissible).
	// For now, return a placeholder value.
	memoryUsedMb = 0 // Placeholder

	log.Printf("Captured output for submission %s. Execution time: %dms, Memory: %dMB (placeholder)", submissionID, executionTimeMs, memoryUsedMb)

	return output, executionTimeMs, memoryUsedMb, nil
}

// PrepareEnvironment creates a temporary directory and writes source code and test case inputs.
// It returns the path to the temporary directory.
func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, testCases []store.TestCase) (tempDir string, err error) {
	log.Printf("Preparing environment for submission %s", submissionID)

	// Create a unique temporary directory
	// Use a base temp dir like os.TempDir() and append submissionID
	tempDir = filepath.Join(os.TempDir(), "judge", submissionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	// Write source code to a file in the temp directory
	sourceFileName := "main.code" // Default, will be overridden by lang config
	// Need to get the correct source file name based on language from LangConfig

	// For now, just use a generic name or determine extension based on 'lang'

	// NOTE: This needs to be updated to use the SourceFileName from LangConfig

	sourceFilePath := filepath.Join(tempDir, sourceFileName)
	if err := os.WriteFile(sourceFilePath, []byte(sourceCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write source code file %s: %w", sourceFilePath, err)
	}
	log.Printf("Wrote source code to %s", sourceFilePath)

	// Write test case input files
	for i, tc := range testCases {
		inputFileName := fmt.Sprintf("input%d.txt", i)
		inputFilePath := filepath.Join(tempDir, inputFileName)
		if err := os.WriteFile(inputFilePath, []byte(tc.Input), 0644); err != nil {
			return "", fmt.Errorf("failed to write test case input file %s: %w", inputFilePath, err)
		}
		log.Printf("Wrote input file %s", inputFilePath)
	}

	log.Printf("Environment prepared in %s", tempDir)
	return tempDir, nil
}

// CleanupEnvironment removes the temporary directory.
func (r *Runner) CleanupEnvironment(tempDir string) error {
	log.Printf("Cleaning up environment: %s", tempDir)
	return os.RemoveAll(tempDir)
}
