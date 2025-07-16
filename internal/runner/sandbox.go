package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Yawn-Sean/project-judger/internal/models"
)

// Execute runs the compiled code directly, reporting CPU time for accuracy.
// It enforces a wall-clock time limit using context for safety.
func (r *Runner) Execute(executablePath string, testCase models.TestCase, timeLimitMs int, memoryLimitMb int) (result models.ExecutionResult) {
	log.Printf("Executing %s with time limit %dms (wall-clock), memory limit %dMB", executablePath, timeLimitMs, memoryLimitMb)

	// Use context for wall-clock timeout
	ctx, cancel := context.WithTimeout(r.Ctx, time.Duration(timeLimitMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, executablePath)
	cmd.Dir = filepath.Dir(executablePath)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		result.Status = "Internal Error"
		result.Error = fmt.Sprintf("failed to create stdin pipe: %v", err)
		return
	}
	go func() {
		defer stdinPipe.Close()
		io.WriteString(stdinPipe, testCase.Input)
	}()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Variables to store results
	var wallClockTime time.Duration
	var cpuTimeMs int
	var memUsageKb uint64

	startTime := time.Now()
	err = cmd.Run()
	wallClockTime = time.Since(startTime)

	// Get resource usage (CPU time, Memory) after command finishes
	if cmd.ProcessState != nil {
		// This works on Linux/macOS.
		if rusage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			memUsageKb = uint64(rusage.Maxrss) // on Linux, this is in KB

			// Calculate total CPU time (user + system) in milliseconds. This is the "correct" competitive programming time.
			userCpuTimeMs := (rusage.Utime.Sec * 1000) + (rusage.Utime.Usec / 1000)
			sysCpuTimeMs := (rusage.Stime.Sec * 1000) + (rusage.Stime.Usec / 1000)
			cpuTimeMs = int(userCpuTimeMs + sysCpuTimeMs)
		}
	}

	// Check for timeout (based on wall-clock time)
	if ctx.Err() == context.DeadlineExceeded {
		result.Status = "Time Limit Exceeded"
		result.ExecutionTimeMs = int(wallClockTime.Milliseconds()) // Report wall-clock time for TLE
		result.MemoryUsedKb = memUsageKb
		log.Printf("Submission timed out. Wall-clock time: %s", wallClockTime)
		return
	}

	// If not timed out, use the more accurate CPU time for reporting.
	result.ExecutionTimeMs = cpuTimeMs
	result.MemoryUsedKb = memUsageKb

	// Check for other runtime errors
	if err != nil {
		result.Status = "Runtime Error"
		result.Error = stderr.String()
		log.Printf("Runtime error for %s. CPU time: %dms. Stderr: %s", executablePath, cpuTimeMs, stderr.String())
		return
	}

	// Success
	result.Status = "Completed"
	result.Output = stdout.String()

	log.Printf("Execution completed for %s. CPU Time: %dms, Memory: %dKB", executablePath, result.ExecutionTimeMs, result.MemoryUsedKb)
	return result
}


// PrepareEnvironment creates a temporary directory and writes the source code file.
func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, lang string) (tempDir string, err error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language for environment preparation: %s", lang)
	}

	tempDir, err = os.MkdirTemp(os.TempDir(), "judgerun-"+submissionID+"-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	sourceFilePath := filepath.Join(tempDir, config.SourceFileName)
	if err := os.WriteFile(sourceFilePath, []byte(sourceCode), 0644); err != nil {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("failed to write source code: %w", err)
	}

	log.Printf("Environment prepared for submission %s in %s", submissionID, tempDir)
	return tempDir, nil
}

// Compile translates source code into an executable file.
func (r *Runner) Compile(tempDir string, lang string) (executablePath string, compileOutput string, err error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", "", fmt.Errorf("unsupported language for compilation: %s", lang)
	}

	if config.CompileCmd == "" {
		return filepath.Join(tempDir, config.SourceFileName), "", nil
	}
	
	ctx, cancel := context.WithTimeout(r.Ctx, 30*time.Second) // 30-second compile timeout
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", config.CompileCmd)
	cmd.Dir = tempDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		log.Printf("Compilation failed for %s: %v. Stderr: %s", tempDir, err, stderr.String())
		return "", stderr.String(), fmt.Errorf("compilation failed: %w", err)
	}

	executablePath = filepath.Join(tempDir, config.ExecutableFileName)
	log.Printf("Compilation successful for %s. Executable at %s", tempDir, executablePath)
	return executablePath, "", nil
}

// CleanUp removes the temporary directory.
func (r *Runner) CleanUp(tempDir string) {
	if err := os.RemoveAll(tempDir); err != nil {
		log.Printf("Warning: failed to clean up temp directory %s: %v", tempDir, err)
	} else {
		log.Printf("Successfully cleaned up directory %s", tempDir)
	}
}
