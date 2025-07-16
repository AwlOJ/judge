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

	"judge-service/internal/config"
	"judge-service/internal/store"
)

// Runner encapsulates the logic for running code.
type Runner struct {
	Ctx        context.Context
	LangConfig map[string]config.Language
}

// NewRunner creates a new runner instance.
func NewRunner(ctx context.Context, langConfig map[string]config.Language) *Runner {
	return &Runner{
		Ctx:        ctx,
		LangConfig: langConfig,
	}
}

// Execute runs the compiled code directly, reporting CPU time for accuracy.
func (r *Runner) Execute(executablePath string, testCase store.TestCase, timeLimitMs int, memoryLimitMb int) (result store.ExecutionResult) {
	log.Printf("Executing %s with time limit %dms (wall-clock), memory limit %dMB", executablePath, timeLimitMs, memoryLimitMb)

	ctx, cancel := context.WithTimeout(r.Ctx, time.Duration(timeLimitMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, executablePath)
	cmd.Dir = filepath.Dir(executablePath)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		result.Status = store.StatusInternalError
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

	var wallClockTime time.Duration
	var cpuTimeMs int
	var memUsageKb uint64

	startTime := time.Now()
	err = cmd.Run()
	wallClockTime = time.Since(startTime)

	if cmd.ProcessState != nil {
		if rusage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			memUsageKb = uint64(rusage.Maxrss)
			userCpuTimeMs := (rusage.Utime.Sec * 1000) + (rusage.Utime.Usec / 1000)
			sysCpuTimeMs := (rusage.Stime.Sec * 1000) + (rusage.Stime.Usec / 1000)
			cpuTimeMs = int(userCpuTimeMs + sysCpuTimeMs)
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.Status = store.StatusTimeLimitExceeded
		result.ExecutionTimeMs = int(wallClockTime.Milliseconds())
		result.MemoryUsedKb = memUsageKb
		log.Printf("Submission timed out. Wall-clock time: %s", wallClockTime)
		return
	}

	result.ExecutionTimeMs = cpuTimeMs
	result.MemoryUsedKb = memUsageKb

	if err != nil {
		result.Status = store.StatusRuntimeError
		result.Error = stderr.String()
		log.Printf("Runtime error for %s. CPU time: %dms. Stderr: %s", executablePath, cpuTimeMs, stderr.String())
		return
	}

	result.Status = store.StatusCompleted
	result.Output = stdout.String()
	log.Printf("Execution completed for %s. CPU Time: %dms, Memory: %dKB", executablePath, result.ExecutionTimeMs, result.MemoryUsedKb)
	return result
}

// PrepareEnvironment creates a temporary directory and writes the source code file.
func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, lang string) (tempDir string, err error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language: %s", lang)
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
	return tempDir, nil
}

// Compile translates source code into an executable file.
func (r *Runner) Compile(tempDir string, lang string) (executablePath string, compileOutput string, err error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", "", fmt.Errorf("unsupported language: %s", lang)
	}

	if config.CompileCmd == "" {
		return filepath.Join(tempDir, config.SourceFileName), "", nil
	}
	
	ctx, cancel := context.WithTimeout(r.Ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", config.CompileCmd)
	cmd.Dir = tempDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", stderr.String(), fmt.Errorf("compilation failed: %w", err)
	}

	return filepath.Join(tempDir, config.ExecutableFileName), "", nil
}

// CleanUp removes the temporary directory.
func (r *Runner) CleanUp(tempDir string) {
	if err := os.RemoveAll(tempDir); err != nil {
		log.Printf("Warning: failed to clean up temp directory %s: %v", tempDir, err)
	}
}
