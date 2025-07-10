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

// Runner handles the execution of code within isolate sandboxes.
type Runner struct {
	// Configuration for language-specific commands
	LangConfig map[string]struct {
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
			CompileCmd         []string
			RunCmd             []string
			SourceFileName     string
			ExecutableFileName string
		}{
			"cpp": {
				CompileCmd:         []string{"g++", "main.cpp", "-o", "main.out", "-O2", "-static", "-Wall"}, // Paths relative to /box
				RunCmd:             []string{"." + string(filepath.Separator) + "main.out"},                                                  // Path relative to /box
				SourceFileName:     "main.cpp",
				ExecutableFileName: "main.out",
			},
			"python": {
				CompileCmd:         nil,                                     // Python is interpreted, no compile step
				RunCmd:             []string{"python3", "main.py"},          // Path relative to /box
				SourceFileName:     "main.py",
				ExecutableFileName: "", // Not applicable for interpreted languages
			},
			// Add configurations for other languages here
		},
	}
}

// Compile compiles the user's source code in an isolate sandbox.
// It returns the path to the compiled executable inside the temp directory (relative to tempDir), or an error.
func (r *Runner) Compile(ctx context.Context, submissionID string, lang string, tempDir string) (executableRelativePath string, compileErr error) {
	log.Printf("Compiling submission %s (lang: %s) in %s using isolate", submissionID, lang, tempDir)

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

	// --- Build the isolate command for compilation ---
	// isolate --box-id=<submissionID> --cg --dir=/etc=RO --cwd=/box --run -- <compile_command...>
	// Note: tempDir on host maps to /box in sandbox
	isolateArgs := []string{
		"--box-id="+submissionID, // Unique box ID
		"--cg",                    // Use cgroups for better resource control
		"--dir=/etc=RO",           // Mount /etc as read-only for security
		"--full-env",              // Provide a full environment (PATH etc.)
		"--cwd=/box",              // Set working directory inside the box to /box
		"--run",                   // Run the following command
		"--",                      // Separator for command and its arguments
	}

	// Append the actual compile command (paths like main.cpp are relative to /box)
	isolateArgs = append(isolateArgs, config.CompileCmd...)

	cmd := exec.CommandContext(ctx, "isolate", isolateArgs...)
	cmd.Dir = tempDir // Set the host working directory, which will be mounted as /box in sandbox

	// Capture stderr for compilation errors
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr // Capture stdout as well for compiler warnings/errors

	log.Printf("Running isolate compile command: isolate %s (in host dir: %s)", strings.Join(isolateArgs, " "), tempDir)

	err := cmd.Run()

	if err != nil {
		execErr := fmt.Errorf("isolate compile command execution failed: %w (stderr: %s)", err, stderr.String())

		if exitErr, ok := err.(*exec.ExitError); ok {
			execErr = fmt.Errorf("compilation failed with exit code %d (stderr: %s)", exitErr.ExitCode(), stderr.String())
		}

		log.Printf("Compilation failed for submission %s: %v", submissionID, execErr)
		return "", execErr // Return error indicating compilation failure
	}

	log.Printf("Compilation successful for submission %s", submissionID)
	return config.ExecutableFileName, nil
}

// Execute runs the compiled code against a single test case in an isolate sandbox.
// It returns the output, execution time, memory usage, and any runtime error.
func (r *Runner) Execute(ctx context.Context, submissionID string, lang string, executableRelativePath string, testCase *store.TestCase, timeLimit int, memoryLimit int, tempDir string) (output string, executionTimeMs int, memoryUsedKb int, runtimeErr error) {
	log.Printf("Executing submission %s (lang: %s) against test case in isolate (TimeLimit: %ds, MemoryLimit: %dMB)", submissionID, lang, timeLimit, memoryLimit)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported language for execution: %s", lang)
	}

	// Write input to file in the temporary directory (which isolate will map to /box)
	inputFileName := "input.txt"
	inputFilePath := filepath.Join(tempDir, inputFileName)
	if err := os.WriteFile(inputFilePath, []byte(testCase.Input), 0644); err != nil {
		return "", 0, 0, fmt.Errorf("failed to write test case input file: %w", err)
	}
	defer os.Remove(inputFilePath) // Clean up host input file

	// Define paths relative to the sandbox's /box directory
	sandboxInputPath := "input.txt"
	sandboxOutputPath := "output.txt"
	sandboxStderrPath := "stderr.txt"
	sandboxMetaPath := "meta.txt" // isolate outputs metadata here

	// Build isolate run command arguments
	// isolate --box-id=<submissionID> --cg --time=<timeLimit> --wall-time=<timeLimit+5> --mem=<memoryLimit> \
	// --fsize=65536 --processes=100 --dir=/etc=RO --stdin=input.txt --stdout=output.txt --stderr=stderr.txt \
	// --meta=meta.txt --cwd=/box --run -- <run_command...>
	isolateArgs := []string{
		"--box-id="+submissionID,
		"--cg", // Use cgroups
		fmt.Sprintf("--time=%d", timeLimit), // CPU time limit in seconds
		fmt.Sprintf("--wall-time=%d", timeLimit+5), // Wall clock time limit (buffer for I/O)
		fmt.Sprintf("--mem=%d", memoryLimit*1024), // Memory limit in KB (convert MB to KB)
		"--fsize=65536", // Max output file size in KB (e.g., 64MB)
		"--processes=100", // Max processes
		"--dir=/etc=RO",
		"--full-env",
		fmt.Sprintf("--stdin=%s", sandboxInputPath),
		fmt.Sprintf("--stdout=%s", sandboxOutputPath),
		fmt.Sprintf("--stderr=%s", sandboxStderrPath),
		fmt.Sprintf("--meta=%s", sandboxMetaPath),
		"--cwd=/box",
		"--run",
		"--",
	}

	// Append the actual run command (paths like main.py or ./main.out are relative to /box)
	isolateArgs = append(isolateArgs, config.RunCmd...)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeLimit+10)*time.Second) // Longer context timeout
	defer cancel()

	cmd := exec.CommandContext(execCtx, "isolate", isolateArgs...)
	cmd.Dir = tempDir // Set the host working directory, which will be mounted as /box in sandbox

	log.Printf("Running isolate execution command: isolate %s (in host dir: %s)", strings.Join(isolateArgs, " "), tempDir)

	err := cmd.Run()

	// --- Read results from files in tempDir on host ---
	outputFilePath := filepath.Join(tempDir, sandboxOutputPath)
	outputBytes, readOutErr := os.ReadFile(outputFilePath)
	defer os.Remove(outputFilePath)
	if readOutErr != nil {
		log.Printf("Error reading output file for submission %s: %v", submissionID, readOutErr)
		output = ""
	} else {
		output = string(outputBytes)
	}

	stderrFilePath := filepath.Join(tempDir, sandboxStderrPath)
	userProgramStderrBytes, _ := os.ReadFile(stderrFilePath)
	defer os.Remove(stderrFilePath)
	userProgramStderr := strings.TrimSpace(string(userProgramStderrBytes))

	metaFilePath := filepath.Join(tempDir, sandboxMetaPath)
	metaBytes, errMeta := os.ReadFile(metaFilePath)
	defer os.Remove(metaFilePath)

	executionTimeMs = 0
	memoryUsedKb = 0
	var isolateExitStatus string // To store reason for isolate exit (e.g., Time Limit Exceeded, Memory Limit Exceeded)

	if errMeta == nil {
		metaContent := string(metaBytes)
		// Parse isolate meta file
		// Example content:
		// time:0.004
		// time-cpu:0.004
		// max-rss:1500
		// exitcode:0
		// status:OK
		lines := strings.Split(metaContent, "
")
		for _, line := range lines {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				switch key {
				case "time-cpu":
					if t, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
						executionTimeMs = int(t * 1000) // Convert seconds to milliseconds
					}
				case "max-rss":
					if m, parseErr := strconv.Atoi(value); parseErr == nil {
						memoryUsedKb = m // isolate reports in KB
					}
				case "status":
					isolateExitStatus = value
				}
			}
		}
	} else {
		log.Printf("Warning: Failed to read isolate meta file for submission %s: %v", submissionID, errMeta)
	}


	// Handle execution errors reported by isolate or exec.CommandContext
	if err != nil {
		// Check isolate's reported status first
		switch isolateExitStatus {
		case "TO": // Time Limit Exceeded (CPU time)
			log.Printf("Execution timed out (CPU) for submission %s. CPU Time: %dms", submissionID, executionTimeMs)
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Time Limit Exceeded")
		case "TL": // Time Limit Exceeded (Wall time)
			log.Printf("Execution timed out (Wall) for submission %s. CPU Time: %dms", submissionID, executionTimeMs)
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Time Limit Exceeded (Wall)")
		case "ML": // Memory Limit Exceeded
			log.Printf("Memory limit exceeded for submission %s. Memory: %dKB", submissionID, memoryUsedKb)
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Memory Limit Exceeded")
		case "RT": // Runtime Error (e.g., segmentation fault, unhandled exception)
			log.Printf("Runtime Error for submission %s. Stderr: %s", submissionID, userProgramStderr)
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Runtime Error: %s", userProgramStderr)
		case "SG": // Signalled (e.g., killed by OS for resource violation, like OOM)
			log.Printf("Process signalled for submission %s. Stderr: %s", submissionID, userProgramStderr)
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Runtime Error (Signalled): %s", userProgramStderr)
		}

		// Fallback for generic exec.CommandContext errors (e.g., isolate not found, context cancelled)
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			log.Printf("Execution context timeout for submission %s (Isolate did not report specific status)", submissionID)
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Time Limit Exceeded (Context)")
		}

		log.Printf("Unexpected isolate error for submission %s: %v (Isolate status: %s)", submissionID, err, isolateExitStatus)
		return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Execution System Error: %v (Isolate status: %s)", err, isolateExitStatus)
	}

	log.Printf("Execution success for submission %s. CPU Time: %dms, Memory: %dKB", submissionID, executionTimeMs, memoryUsedKb)
	return output, executionTimeMs, memoryUsedKb, nil
}

// PrepareEnvironment creates a temporary directory and writes source code and test case inputs.
// It returns the path to the temporary directory.
func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, testCases []store.TestCase, lang string) (tempDir string, err error) {
	log.Printf("Preparing environment for submission %s (lang: %s)", submissionID, lang)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language for environment preparation: %s", lang)
	}

	// Create a unique temporary directory for isolate box
	// This directory will be mounted as /box inside the isolate sandbox
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
		inputFileName := fmt.Sprintf("input%d.txt", i)
		inputFilePath := filepath.Join(tempDir, inputFileName)
		if err := os.WriteFile(inputFilePath, []byte(tc.Input), 0644); err != nil {
			return "", fmt.Errorf("failed to write test case input file: %w", inputFilePath, err)
		}
		log.Printf("Wrote input file %s", inputFilePath)
	}

	log.Printf("Environment prepared in %s", tempDir)
	return tempDir, nil
}

// CleanupEnvironment removes the temporary directory.
func (r *Runner) CleanupEnvironment(tempDir string) error {
	log.Printf("Cleaning up environment: %s", tempDir)

	// isolate requires the box to be cleaned up manually before deleting the directory
	// We use --box-id and clean up the box corresponding to the submission ID
	cmd := exec.Command("isolate", "--box-id="+filepath.Base(tempDir), "--cleanup")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cleanErr := cmd.Run()
	if cleanErr != nil {
		log.Printf("Warning: Failed to cleanup isolate box %s: %v (stdout: %s, stderr: %s)", filepath.Base(tempDir), cleanErr, stdout.String(), stderr.String())
	}

	// Then remove the temporary directory
	return os.RemoveAll(tempDir)
}
