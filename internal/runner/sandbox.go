//go:build linux

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
)

// Runner handles the execution of code within a secure sandbox using firejail.
type Runner struct {
	LangConfig map[string]struct {
		CompilerPath       string
		CompileCmd         []string
		RunCmd             []string
		SourceFileName     string
		ExecutableFileName string
	}
	FirejailPath string
}

// NewRunner creates a new Runner instance.
func NewRunner() (*Runner, error) {
	firejailPath, err := exec.LookPath("firejail")
	if err != nil {
		return nil, fmt.Errorf("firejail executable not found in PATH: %w. Make sure it is installed", err)
	}

	return &Runner{
		FirejailPath: firejailPath,
		LangConfig: map[string]struct {
			CompilerPath       string
			CompileCmd         []string
			RunCmd             []string
			SourceFileName     string
			ExecutableFileName string
		}{
			"cpp": {
				CompilerPath:       "/usr/bin/g++",
				CompileCmd:         []string{"/usr/bin/g++", "main.cpp", "-o", "main.out", "-O2", "-static", "-Wall"},
				RunCmd:             []string{"./main.out"},
				SourceFileName:     "main.cpp",
				ExecutableFileName: "main.out",
			},
			"python": {
				CompilerPath:       "",
				CompileCmd:         nil,
				RunCmd:             []string{"python3", "./main.py"},
				SourceFileName:     "main.py",
				ExecutableFileName: "",
			},
		},
	}, nil
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
		return "", fmt.Errorf("failed to write source code: %w", err)
	}

	log.Printf("Environment prepared for submission %s in %s", submissionID, tempDir)
	return tempDir, nil
}

// Compile compiles the source code inside the temporary directory.
func (r *Runner) Compile(ctx context.Context, submissionID string, lang string, tempDir string) (string, error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language for compilation: %s", lang)
	}

	if config.CompileCmd == nil {
		log.Printf("Language %s is interpreted, no compilation needed.", lang)
		return filepath.Join(tempDir, config.SourceFileName), nil
	}

	cmd := exec.CommandContext(ctx, config.CompilerPath, config.CompileCmd[1:]...)
	cmd.Dir = tempDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	log.Printf("Running compile command: %s in %s", strings.Join(cmd.Args, " "), cmd.Dir)

	// THE FIX IS HERE: Actually run the command and check the error.
	if err := cmd.Run(); err != nil {
		execErr := fmt.Errorf("compilation failed (stderr: %s)", stderr.String())
		log.Printf("Compilation failed for submission %s: %v", submissionID, execErr)
		return "", execErr
	}

	executablePath := filepath.Join(tempDir, config.ExecutableFileName)
	log.Printf("Compilation successful for submission %s. Executable at: %s", submissionID, executablePath)
	return executablePath, nil
}

// Execute runs the code against a single test case using firejail.
func (r *Runner) Execute(ctx context.Context, submissionID, lang, executablePath string, testCase *store.TestCase, timeLimit int, memoryLimit int) (output string, execTimeMs int, memoryUsedKb int, runtimeErr error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported language for execution: %s", lang)
	}

	tempDir := filepath.Dir(executablePath)

	// --- Build the firejail command ---
	args := []string{
		"--quiet",
		"--noprofile",                          // Start with a clean slate, no default profiles.
		"--net=none",                           // No network access.
		"--private",                            // Isolate the filesystem.
		fmt.Sprintf("--whitelist=%s", tempDir), // CRITICAL: Only allow access to our temporary directory.
		fmt.Sprintf("--rlimit-cpu=%d", timeLimit),
		fmt.Sprintf("--rlimit-as=%d", memoryLimit*1024*1024),
	}

	args = append(args, config.RunCmd...)

	cmd := exec.CommandContext(ctx, r.FirejailPath, args...)
	// CRITICAL: Set the working directory for the command to our tempDir.
	// This ensures that "./main.out" is found.
	cmd.Dir = tempDir

	cmd.Stdin = strings.NewReader(testCase.Input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Running firejail command: %s %s (in dir: %s)", r.FirejailPath, strings.Join(args, " "), tempDir)

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)
	execTimeMs = int(duration.Milliseconds())

	if err != nil {
		errMsg := ""
		// The error from firejail/the program will be on stderr
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("Runtime Error: %s", stderr.String())
		} else {
			errMsg = fmt.Sprintf("Runtime Error: %v", err)
		}

		// Firejail combined with `rlimit-cpu` will cause the process to be killed by the kernel
		// which results in an exit code like 137 (SIGKILL) or similar.
		if ctx.Err() == context.DeadlineExceeded || strings.Contains(err.Error(), "signal") {
			errMsg = "Time Limit Exceeded"
		}

		return "", execTimeMs, 0, fmt.Errorf(errMsg)
	}

	log.Printf("Execution success for submission %s. Time: %dms", submissionID, execTimeMs)
	return stdout.String(), execTimeMs, 0, nil
}

// CleanupEnvironment removes the temporary directory.
func (r *Runner) CleanupEnvironment(tempDir string) error {
	log.Printf("Cleaning up environment: %s", tempDir)
	return os.RemoveAll(tempDir)
}
