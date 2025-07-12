package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"judge-service/internal/store"
)

type Runner struct {
	LangConfig map[string]struct {
		CompileCmd         []string
		RunCmd             []string
		SourceFileName     string
		ExecutableFileName string
	}
}

func NewRunner() *Runner {
	return &Runner{
		LangConfig: map[string]struct {
			CompileCmd         []string
			RunCmd             []string
			SourceFileName     string
			ExecutableFileName string
		}{
			"cpp": {
				CompileCmd:         []string{"g++", "main.cpp", "-o", "main.out", "-O2", "-static", "-Wall"},
				RunCmd:             []string{"./main.out"},
				SourceFileName:     "main.cpp",
				ExecutableFileName: "main.out",
			},
			"python": {
				CompileCmd:         nil,
				RunCmd:             []string{"python3", "main.py"},
				SourceFileName:     "main.py",
				ExecutableFileName: "",
			},
		},
	}
}

// generateBoxID generates a numeric box ID (0–99) from submission ID
func generateBoxID(submissionID string) string {
	h := fnv.New32a()
	h.Write([]byte(submissionID))
	return fmt.Sprintf("%d", h.Sum32()%100)
}

func (r *Runner) Compile(ctx context.Context, submissionID string, lang string, tempDir string) (string, error) {
	log.Printf("Compiling submission %s (lang: %s) in %s using isolate", submissionID, lang, tempDir)
	boxID := generateBoxID(submissionID)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language: %s", lang)
	}

	if len(config.CompileCmd) == 0 {
		log.Printf("Language %s is interpreted, skipping compilation", lang)
		return config.SourceFileName, nil
	}

	args := []string{
		"--box-id=" + boxID,
		"--cg",
		"--full-env",
		"--dir=/sys/fs/cgroup:/sys/fs/cgroup",
		"--run",
		"--",
	}
	args = append(args, config.CompileCmd...)

	cmd := exec.CommandContext(ctx, "isolate", args...)
	cmd.Dir = tempDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr

	log.Printf("Running compile: isolate %s", strings.Join(args, " "))

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("compilation failed: %w (stderr: %s)", err, stderr.String())
	}

	log.Printf("Compilation succeeded for %s", submissionID)
	return config.ExecutableFileName, nil
}

func (r *Runner) Execute(ctx context.Context, submissionID string, lang string, executableRelativePath string, testCase *store.TestCase, timeLimit int, memoryLimit int, tempDir string) (string, int, int, error) {
	log.Printf("Running submission %s in isolate", submissionID)
	boxID := generateBoxID(submissionID)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", 0, 0, fmt.Errorf("unsupported language: %s", lang)
	}

	inputFile := filepath.Join(tempDir, "input.txt")
	if err := os.WriteFile(inputFile, []byte(testCase.Input), 0644); err != nil {
		return "", 0, 0, err
	}
	defer os.Remove(inputFile)

	args := []string{
		"--box-id=" + boxID,
		"--cg",
		fmt.Sprintf("--time=%d", timeLimit),
		fmt.Sprintf("--wall-time=%d", timeLimit+5),
		fmt.Sprintf("--mem=%d", memoryLimit*1024),
		"--fsize=65536",
		"--processes=100",
		"--dir=/sys/fs/cgroup:/sys/fs/cgroup",
		"--full-env",
		"--stdin=input.txt",
		"--stdout=output.txt",
		"--stderr=stderr.txt",
		"--meta=meta.txt",
		"--run",
		"--",
	}
	args = append(args, config.RunCmd...)

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeLimit+10)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "isolate", args...)
	cmd.Dir = tempDir

	log.Printf("Running isolate command: isolate %s", strings.Join(args, " "))

	err := cmd.Run()

	outputBytes, _ := os.ReadFile(filepath.Join(tempDir, "output.txt"))
	stderrBytes, _ := os.ReadFile(filepath.Join(tempDir, "stderr.txt"))
	metaBytes, _ := os.ReadFile(filepath.Join(tempDir, "meta.txt"))
	os.Remove(filepath.Join(tempDir, "output.txt"))
	os.Remove(filepath.Join(tempDir, "stderr.txt"))
	os.Remove(filepath.Join(tempDir, "meta.txt"))

	output := string(outputBytes)
	userStderr := string(stderrBytes)

	executionTimeMs := 0
	memoryUsedKb := 0
	status := ""

	lines := strings.Split(string(metaBytes), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.TrimSpace(parts[1])
		switch key {
		case "time-cpu":
			if t, _ := strconv.ParseFloat(val, 64); t > 0 {
				executionTimeMs = int(t * 1000)
			}
		case "max-rss":
			if m, _ := strconv.Atoi(val); m > 0 {
				memoryUsedKb = m
			}
		case "status":
			status = val
		}
	}

	if err != nil {
		switch status {
		case "TO", "TL":
			return "", executionTimeMs, memoryUsedKb, errors.New("Time Limit Exceeded")
		case "ML":
			return "", executionTimeMs, memoryUsedKb, errors.New("Memory Limit Exceeded")
		case "SG", "RE", "XX":
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Runtime Error: %s", userStderr)
		default:
			if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
				return "", executionTimeMs, memoryUsedKb, errors.New("Time Limit Exceeded")
			}
			return "", executionTimeMs, memoryUsedKb, fmt.Errorf("Execution Failed: %v", err)
		}
	}

	return output, executionTimeMs, memoryUsedKb, nil
}

func (r *Runner) PrepareEnvironment(submissionID string, sourceCode string, testCases []store.TestCase, lang string) (string, error) {
	log.Printf("Preparing environment for submission %s", submissionID)

	config, ok := r.LangConfig[lang]
	if !ok {
		return "", fmt.Errorf("unsupported language: %s", lang)
	}

	tempDir := filepath.Join(os.TempDir(), "judge", submissionID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", err
	}

	sourcePath := filepath.Join(tempDir, config.SourceFileName)
	if err := os.WriteFile(sourcePath, []byte(sourceCode), 0644); err != nil {
		return "", err
	}

	for i, tc := range testCases {
		inputPath := filepath.Join(tempDir, fmt.Sprintf("input%d.txt", i))
		if err := os.WriteFile(inputPath, []byte(tc.Input), 0644); err != nil {
			return "", err
		}
	}

	log.Printf("Environment ready at %s", tempDir)
	return tempDir, nil
}

func (r *Runner) CleanupEnvironment(tempDir string) error {
	boxID := generateBoxID(filepath.Base(tempDir))
	cmd := exec.Command("isolate", "--box-id="+boxID, "--cleanup")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to cleanup box %s: %s", boxID, out.String())
	}
	return os.RemoveAll(tempDir)
}
