package core

import (
	"strings"
)

// CompareOutputs compares the actual output with the expected output after normalization.
// It returns true if they match, false otherwise.
func CompareOutputs(actualOutput, expectedOutput string) bool {
	// Normalize outputs: trim trailing whitespace and standardize line endings (LF)
	normalize := func(s string) string {
		// Replace Windows line endings (CRLF) with Unix line endings (LF)
		s = strings.ReplaceAll(s, "\r\n", "\n")
		// Split into lines, trim trailing whitespace from each line, and rejoin
		lines := strings.Split(s, "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " \t\r")
		}
		s = strings.Join(lines, "\n")
		// Trim trailing newlines from the whole output
		s = strings.TrimRight(s, "\n")
		return s
	}

	normalizedActual := normalize(actualOutput)
	normalizedExpected := normalize(expectedOutput)

	// Compare the normalized outputs
	return normalizedActual == normalizedExpected
}

// Note: More complex comparison logic (e.g., for floating point numbers with tolerance)
// would be added here or in separate functions as needed.