package runner

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"errors"
	"judge-service/internal/store"
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
				Image:              "gcc-with-time",
				CompileCmd:         []string{"g++", "/workspace/main.cpp", "-o", "/workspace/main.out", "-O2", "-static", "-Wall"},
				RunCmd:             []string{"/workspace/main.out"}, // Just the executable
				SourceFileName:     "main.cpp",
				ExecutableFileName: "main.out",
			},
			"python": {
				Image:              "python-with-time",
				CompileCmd:         nil,                                       // Python is interpreted, no compile step
				RunCmd:             []string{"python3", "/workspace/main.py"}, // Just the interpreter and script
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
func (r *Runner) Execute(ctx context.Context, submissionID string, lang string, executableRelativePath string, testCase *store.TestCase, timeLimit int, memoryLimit int, tempDir string) (output string, executionTimeMs int, memoryUsedKb int, runtimeErr error) {
	log.Printf("Executing submission %s (lang: %s) against test case (TimeLimit: %ds, MemoryLimit: %dMB)", submissionID, lang, timeLimit, memoryLimit)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported language for execution: %s", lang)
	}

	// Write input to file
	inputFilePath := filepath.Join(tempDir, "input.txt")
	if err := os.WriteFile(inputFilePath, []byte(testCase.Input), 0644); err != nil {
		return "", 0, 0, fmt.Errorf("failed to write test case input file: %w", err)
	}
	defer os.Remove(inputFilePath)

	// Define container paths
	workspaceInputPath := "/workspace/input.txt"
	workspaceOutputPath := "/workspace/output.txt"
	workspaceStderrPath := "/workspace/stderr.txt"
	workspaceTimePath := "/workspace/time.txt"

	// Build docker run command
	args := []string{
		"run", "--rm",
		"--network=none",
		fmt.Sprintf("--cpus=%f", float64(1.0)),
		fmt.Sprintf("--memory=%dm", memoryLimit),
		"-v", fmt.Sprintf("%s:/workspace", tempDir),
		config.Image,
	}

	// Build the shell command inside container
	runCommand := strings.Join(config.RunCmd, " ")
	shellCmd := fmt.Sprintf(
		"/usr/bin/time -f \"%%e\" -o %s timeout %ds %s < %s > %s 2> %s",
		workspaceTimePath,
		timeLimit,
		runCommand,
		workspaceInputPath,
		workspaceOutputPath,
		workspaceStderrPath,
	)
	args = append(args, "bash", "-c", shellCmd)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeLimit+5)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "docker", args...)
	var dockerStderr bytes.Buffer
	cmd.Stderr = &dockerStderr

	log.Printf("Running execution command: docker %s", strings.Join(args, " "))

	err := cmd.Run()

	// Read stderr output of the user's program
	stderrFilePath := filepath.Join(tempDir, "stderr.txt")
	userProgramStderrBytes, _ := os.ReadFile(stderrFilePath)
	defer os.Remove(stderrFilePath)
	userProgramStderr := strings.TrimSpace(string(userProgramStderrBytes))

	// Read output file
	outputFilePath := filepath.Join(tempDir, "output.txt")
	outputBytes, readOutErr := os.ReadFile(outputFilePath)
	defer os.Remove(outputFilePath)
	if readOutErr != nil {
		log.Printf("Error reading output file for submission %s: %v", submissionID, readOutErr)
		output = ""
	} else {
		output = string(outputBytes)
	}

	// Read execution time from time.txt
	timeFilePath := filepath.Join(tempDir, "time.txt")
	timeBytes, errTime := os.ReadFile(timeFilePath)
	defer os.Remove(timeFilePath)
	if errTime != nil {
		log.Printf("Failed to read time.txt for submission %s: %v", submissionID, errTime)
		executionTimeMs = -1
	} else {
		timeStr := strings.TrimSpace(string(timeBytes))
		if timeSec, errParse := strconv.ParseFloat(timeStr, 64); errParse == nil {
			executionTimeMs = int(timeSec * 1000)
		} else {
			log.Printf("Failed to parse time.txt for submission %s: %v", submissionID, errParse)
			executionTimeMs = -1
		}
	}

	// Handle execution errors
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 124 {
				log.Printf("Execution timed out for submission %s. Docker stderr: %s", submissionID, dockerStderr.String())
				return "", executionTimeMs, 0, fmt.Errorf("Time Limit Exceeded")
			}
			log.Printf("Execution failed with exit code %d for submission %s. Stderr: %s", exitErr.ExitCode(), submissionID, userProgramStderr)
			return "", executionTimeMs, 0, fmt.Errorf("Runtime Error (Exit Code %d): %s", exitErr.ExitCode(), userProgramStderr)
		}

		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			log.Printf("Execution context timeout for submission %s", submissionID)
			return "", executionTimeMs, 0, fmt.Errorf("Time Limit Exceeded (Context)")
		}

		log.Printf("Unexpected docker error for submission %s: %v", submissionID, err)
		return "", executionTimeMs, 0, fmt.Errorf("Execution System Error: %v", err)
	}

	log.Printf("Execution success for submission %s. Time: %dms", submissionID, executionTimeMs)
	return output, executionTimeMs, 0, nil // memoryUsedKb = 0 (placeholder)
}

// PrepareEnvironment creates a temporary directory and writes source code and test case inputs.
// It returns the path to the temporary directory.
func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, testCases []store.TestCase, lang string) (tempDir string, err error) {
	log.Printf("Preparing environment for submission %s (lang: %s)", submissionID, lang)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language for environment preparation: %s", lang)
	}

	// Create a unique temporary directory
	tempDir = filepath.Join(os.TempDir(), "judge", submissionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	// Write source code to a file in the temp directory, using language-specific file name
	sourceFilePath := filepath.Join(tempDir, config.SourceFileName)
	if err := os.WriteFile(sourceFilePath, []byte(sourceCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write source code file %s: %w", sourceFilePath, err)
	}
	log.Printf("Wrote source code to %s", sourceFilePath)

	// Write test case input files
	for i, tc := range testCases {
		inputFileName := fmt.Sprintf("input%d.txt", i) // Using a generic input filename for now as testCase is passed directly
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
