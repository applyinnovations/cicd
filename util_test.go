package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestAddDozzleGroupLabel(t *testing.T) {
	originalFile := "test.yml"
	backupFile := "test_backup.yml"
	expectedFile := "expected.yml"

	// Backup the original file
	if err := copyFile(originalFile, backupFile); err != nil {
		t.Fatalf("Failed to backup original file: %v", err)
	}

	// Ensure that the original file is restored regardless of test outcome
	defer func() {
		if err := os.Remove(originalFile); err != nil {
			t.Errorf("Failed to remove modified file: %v", err)
		}
		if err := os.Rename(backupFile, originalFile); err != nil {
			t.Errorf("Failed to restore original file from backup: %v", err)
		}
	}()

	// Run the function being tested
	if err := addDozzleGroupLabel(originalFile, "test-label"); err != nil {
		t.Fatalf(`addDozzleGroupLabel("%s", "test-label") returned error: %v`, originalFile, err)
	}

	// Compare the modified file with the expected file
	different, diffOutput, err := compareFiles(expectedFile, originalFile)
	if err != nil {
		t.Fatalf("Failed to compare files: %v", err)
	}

	if different {
		t.Errorf("Modified file does not match expected file:\n%s", diffOutput)
	}
}

// copyFile copies the contents of the src file to dst file.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open source file: %w", err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("cannot create destination file: %w", err)
	}
	defer destinationFile.Close()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// Copy file permissions
	if info, err := os.Stat(src); err == nil {
		if err := os.Chmod(dst, info.Mode()); err != nil {
			return fmt.Errorf("failed to set file permissions: %w", err)
		}
	}

	return nil
}

func compareFiles(expectedPath, actualPath string) (bool, string, error) {
	expectedFile, err := os.Open(expectedPath)
	if err != nil {
		return false, "", fmt.Errorf("cannot open expected file: %w", err)
	}
	defer expectedFile.Close()

	actualFile, err := os.Open(actualPath)
	if err != nil {
		return false, "", fmt.Errorf("cannot open actual file: %w", err)
	}
	defer actualFile.Close()

	expectedScanner := bufio.NewScanner(expectedFile)
	actualScanner := bufio.NewScanner(actualFile)

	different := false
	var diffOutput string

	lineNumber := 1
	for expectedScanner.Scan() {
		actualHasNext := actualScanner.Scan()
		expectedLine := expectedScanner.Text()
		actualLine := ""
		if actualHasNext {
			actualLine = actualScanner.Text()
		}

		if !actualHasNext || expectedLine != actualLine {
			different = true
			if expectedLine != actualLine {
				diffOutput += fmt.Sprintf("\x1b[31m- %d: %s\x1b[0m\n", lineNumber, expectedLine)
				if actualHasNext {
					diffOutput += fmt.Sprintf("\x1b[32m+ %d: %s\x1b[0m\n", lineNumber, actualLine)
				} else {
					diffOutput += fmt.Sprintf("\x1b[32m+ %d: [missing line]\x1b[0m\n", lineNumber)
				}
			}
		}
		lineNumber++
	}

	// Check for any remaining lines in the actual file
	for actualScanner.Scan() {
		different = true
		actualLine := actualScanner.Text()
		diffOutput += fmt.Sprintf("\x1b[31m- %d: [missing line]\x1b[0m\n", lineNumber)
		diffOutput += fmt.Sprintf("\x1b[32m+ %d: %s\x1b[0m\n", lineNumber, actualLine)
		lineNumber++
	}

	if err := expectedScanner.Err(); err != nil {
		return false, "", fmt.Errorf("error reading expected file: %w", err)
	}

	if err := actualScanner.Err(); err != nil {
		return false, "", fmt.Errorf("error reading actual file: %w", err)
	}

	return different, diffOutput, nil
}
