package trace

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTracer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "trace_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tracer, err := NewTracer(tempDir, true)
	if err != nil {
		t.Fatalf("failed to create tracer: %v", err)
	}
	defer tracer.Close()

	msg := "test log message"
	tracer.Trace(msg, nil)

	errMsg := "test error message"
	testErr := errors.New(errMsg)
	tracer.Trace("error occurred", testErr)

	// Give a small amount of time for the file to be flushed if needed
	time.Sleep(100 * time.Millisecond)

	// More specific check for the expected file path structure
	now := time.Now()
	expectedFile := fmt.Sprintf(fstamp, tempDir, now.Year(), now.Month(), now.Day(), now.Hour())
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Errorf("expected log file %s does not exist", expectedFile)
	} else {
		content, _ := os.ReadFile(expectedFile)
		if !strings.Contains(string(content), msg) {
			t.Errorf("log file does not contain expected message: %s", msg)
		}
		if !strings.Contains(string(content), errMsg) {
			t.Errorf("log file does not contain expected error message: %s", errMsg)
		}
	}
}

func TestLegacyTrace(t *testing.T) {
	// This test uses the package-level Trace which uses defaultTracer
	// We just ensure it doesn't crash and output something
	Trace("testing legacy trace", nil)
	Trace("testing legacy error trace", errors.New("legacy error"))
}
