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
	"strconv"
	"strings"
	"syscall"
	"time"

	"judge-service/internal/store"
)

// Runner handles the execution of code within a secure sandbox using nsjail.
type Runner struct {
	LangConfig map[string]struct {
		CompilerPath       string
		CompileCmd         []string
		RunCmd             []string
		SourceFileName     string
		ExecutableFileName string
	}
	NsjailPath        string
	NsjailConfigPath  string
}

// NewRunner creates a new Runner instance.
func NewRunner() (*Runner, error) {
	nsjailPath, err := exec.LookPath("nsjail")
	if err != nil {
		return nil, fmt.Errorf("nsjail executable not found in PATH: %w", err)
	}

	return &Runner{
		NsjailPath:        nsjailPath,
		NsjailConfigPath:  "/etc/nsjail.cfg", // Standard path for our config
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
				RunCmd:             []string{"/app/main.out"},
				SourceFileName:     "main.cpp",
				ExecutableFileName: "main.out",
			},
			"python": {
				CompilerPath:       "", // Not a compiled language
				CompileCmd:         nil,
				RunCmd:             []string{"/usr/bin/python3", "/app/main.py"},
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
		return config.SourceFileName, nil
	}

	cmd := exec.CommandContext(ctx, config.CompilerPath, config.CompileCmd[1:]...)
	cmd.Dir = tempDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	log.Printf("Running compile command: %s in %s", strings.Join(cmd.Args, " "), cmd.Dir)

	if err := cmd.Run(); err != nil {
		execErr := fmt.Errorf("compilation failed (stderr: %s)", stderr.String())
		log.Printf("Compilation failed for submission %s: %v", submissionID, execErr)
		return "", execErr
	}

	executablePath := filepath.Join(tempDir, config.ExecutableFileName)
	log.Printf("Compilation successful for submission %s. Executable at: %s", submissionID, executablePath)
	return executablePath, nil
}

// Execute runs the code against a single test case using nsjail.
func (r *Runner) Execute(ctx context.Context, submissionID, lang, executablePath string, testCase *store.TestCase, timeLimit int, memoryLimit int) (output string, execTimeMs int, memoryUsedKb int, runtimeErr error) {
	config, ok := r.LangConfig[lang]
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported language for execution: %s", lang)
	}

	tempDir := filepath.Dir(executablePath)
	
	inputFile := filepath.Join(tempDir, "input.txt")
	outputFile := filepath.Join(tempDir, "output.txt")
	stderrFile := filepath.Join(tempDir, "stderr.txt")

	if err := os.WriteFile(inputFile, []byte(testCase.Input), 0644); err != nil {
		return "", 0, 0, fmt.Errorf("failed to write input file: %w", err)
	}
	os.WriteFile(outputFile, []byte{}, 0644)
	os.WriteFile(stderrFile, []byte{}, 0644)

	// --- Build the nsjail command ---
	args := []string{
		"--config", r.NsjailConfigPath,
		// Override specific limits from the config file
		"--time_limit", strconv.Itoa(timeLimit),
		"--rlimit_as", strconv.Itoa(memoryLimit), // in MB
		// Mount the temporary user code directory as read-write
		"--bindmount", fmt.Sprintf("%s:/app", tempDir),
		"--cwd", "/app",
		"--stdin", "/app/input.txt",
		"--stdout", "/app/output.txt",
		"--stderr", "/app/stderr.txt",
	}
	
	args = append(args, "--")
	args = append(args, config.RunCmd...)

	cmd := exec.CommandContext(ctx, r.NsjailPath, args...)
	
	var nsjailStderr bytes.Buffer
	cmd.Stderr = &nsjailStderr
	
	log.Printf("Running nsjail command: %s %s", r.NsjailPath, strings.Join(args, " "))
	
	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)
	execTimeMs = int(duration.Milliseconds())

	outputBytes, _ := os.ReadFile(outputFile)
	output = string(outputBytes)
	stderrBytes, _ := os.ReadFile(stderrFile)
	userStderr := strings.TrimSpace(string(stderrBytes))

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			ws := exitErr.Sys().(syscall.WaitStatus)
			exitCode := ws.ExitStatus()

			if ws.Signaled() {
				sig := ws.Signal()
				if sig == syscall.SIGXCPU {
					return "", timeLimit * 1000, 0, fmt.Errorf("Time Limit Exceeded")
				}
				if sig == syscall.SIGKILL {
					return "", execTimeMs, 0, fmt.Errorf("Memory Limit Exceeded or other fatal error")
				}
			}

			errMsg := fmt.Sprintf("Runtime Error (Exit Code %d)", exitCode)
			if userStderr != "" {
				errMsg = fmt.Sprintf("%s: %s", errMsg, userStderr)
			}
			return "", execTimeMs, 0, fmt.Errorf(errMsg)
		}
		return "", execTimeMs, 0, fmt.Errorf("sandbox execution failed: %w (nsjail stderr: %s)", err, nsjailStderr.String())
	}

	log.Printf("Execution success for submission %s. Time: %dms", submissionID, execTimeMs)
	return output, execTimeMs, 0, nil
}


// CleanupEnvironment removes the temporary directory.
func (r *Runner) CleanupEnvironment(tempDir string) error {
	log.Printf("Cleaning up environment: %s", tempDir)
	return os.RemoveAll(tempDir)
}
