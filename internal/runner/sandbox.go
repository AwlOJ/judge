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
	NsjailPath string
}

// NewRunner creates a new Runner instance.
func NewRunner() (*Runner, error) {
	nsjailPath, err := exec.LookPath("nsjail")
	if err != nil {
		return nil, fmt.Errorf("nsjail executable not found in PATH: %w. Make sure it is installed and in the system's PATH", err)
	}

	return &Runner{
		NsjailPath: nsjailPath,
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

	// The command will run inside the tempDir.
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

	// The temp directory is the parent of the executable path.
	tempDir := filepath.Dir(executablePath)
	if lang == "python" { // For interpreted languages, executablePath is the source file.
		tempDir = filepath.Dir(executablePath)
	}

	// Create files for stdin, stdout, stderr for the sandboxed process.
	inputFile, err := os.Create(filepath.Join(tempDir, "input.txt"))
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to create input file: %w", err)
	}
	inputFile.WriteString(testCase.Input)
	inputFile.Close()

	outputFile := filepath.Join(tempDir, "output.txt")
	stderrFile := filepath.Join(tempDir, "stderr.txt")

	// --- Build the nsjail command ---
	// nsjail mounts the temp directory containing the executable and IO files as /app
	// It uses cgroups for resource limits and namespaces for isolation.
	args := []string{
		"--mode", "o", // Once: execute the command and exit.
		"--config", "/etc/nsjail/defaults.cfg", // Use a default configuration if available
		"--quiet",
		"--bindmount_ro", fmt.Sprintf("%s:/app", tempDir), // Mount temp dir as read-only inside sandbox
		"--chroot", "/", // The new root will be the real root
		"--cwd", "/app", // Set current working directory inside sandbox
		"--time_limit", strconv.Itoa(timeLimit),
		"--rlimit_as", strconv.Itoa(memoryLimit), // RLIMIT_AS in MB
		"--rlimit_cpu", strconv.Itoa(timeLimit),
		"--rlimit_fsize", "64", // Limit file size creation to 64MB
		"--rlimit_nofile", "10", // Limit number of open file descriptors
		"--proc_ro", // Mount /proc as read-only
		"--iface_no_lo", // No network interfaces, including loopback
	}

	// Determine the actual command to run inside the sandbox.
	runCmd := config.RunCmd
	if lang == "python" {
		// For python, the executable path is the source file itself.
		// We need to adjust the path to be relative to the sandbox mount.
		runCmd = []string{"/usr/bin/python3", filepath.Join("/app", config.SourceFileName)}
	}

	// Redirect I/O inside the shell command executed by nsjail.
	shellCmd := fmt.Sprintf("%s < input.txt > output.txt 2> stderr.txt", strings.Join(runCmd, " "))
	
	// Add the command to run inside nsjail
	args = append(args, "--", "/bin/bash", "-c", shellCmd)

	cmd := exec.CommandContext(ctx, r.NsjailPath, args...)
	
	var nsjailStderr bytes.Buffer
	cmd.Stderr = &nsjailStderr // Capture nsjail's own stderr, not the user's program
	
	log.Printf("Running nsjail command: %s %s", r.NsjailPath, strings.Join(args, " "))
	
	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)
	execTimeMs = int(duration.Milliseconds())

	// Read user program's output and stderr from files.
	outputBytes, _ := os.ReadFile(outputFile)
	output = string(outputBytes)
	stderrBytes, _ := os.ReadFile(stderrFile)
	userStderr := strings.TrimSpace(string(stderrBytes))

	if err != nil {
		// Analyze the exit code to determine the error type.
		if exitErr, ok := err.(*exec.ExitError); ok {
			ws := exitErr.Sys().(syscall.WaitStatus)
			exitCode := ws.ExitStatus()

			// nsjail specific exit codes for TLE/MLE can be found in its documentation.
			// E.g., POSIX exit code for timeout is often 124, nsjail might use others.
			// SIGKILL (9) or SIGXCPU (24) can indicate TLE.
			// SIGSEGV (11) can indicate MLE or other runtime errors.
			switch exitCode {
			case 109: // Corresponds to SIGXCPU from nsjail, likely TLE
				return "", timeLimit * 1000, 0, fmt.Errorf("Time Limit Exceeded")
			case 137: // Corresponds to SIGKILL, could be OOM killer
				return "", execTimeMs, 0, fmt.Errorf("Memory Limit Exceeded")
			default:
				errMsg := fmt.Sprintf("Runtime Error (Exit Code %d)", exitCode)
				if userStderr != "" {
					errMsg = fmt.Sprintf("%s: %s", errMsg, userStderr)
				}
				return "", execTimeMs, 0, fmt.Errorf(errMsg)
			}
		}
		// A different kind of error (e.g., nsjail config error).
		return "", execTimeMs, 0, fmt.Errorf("sandbox execution failed: %w (nsjail stderr: %s)", err, nsjailStderr.String())
	}

	// TODO: Parse memory usage. This is more complex with nsjail and might require cgroup v2 memory.stat.
	// For now, we return 0.
	log.Printf("Execution success for submission %s. Time: %dms", submissionID, execTimeMs)
	return output, execTimeMs, 0, nil
}


// CleanupEnvironment removes the temporary directory.
func (r *Runner) CleanupEnvironment(tempDir string) error {
	log.Printf("Cleaning up environment: %s", tempDir)
	return os.RemoveAll(tempDir)
}
