package trace

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/fatih/color"
)

const (
	placeholder = "%s:->[%s]:%d|%v\n"
	defaultPath = "./data/logs/log.log"
	xflag       = "[ERROR]:"
	lflag       = "[LOG]:"
	fstamp      = "%v/%v/%v/%v/%v.log"
	tracedir    = "./data/logs"

	defaultX = "Could not log to file:"
	dirX     = "failed to create the directory: %v"
	fileX    = "failed to create the file: %v"
	xcontext = "context is empty (int)"

	fcontext    = "\nFields for %s.%s\n"
	fkey        = "field name: %+v\n"
	fvalue      = "value: %+v\n"
	logInterval = 1 * time.Minute
)

// Tracer encapsulates the logging state and logic.
type Tracer struct {
	mu         sync.RWMutex
	verbose    bool
	logDir     string
	loggerPath string
	xLogger    *log.Logger
	logFile    *os.File
	stopChan   chan struct{}
}

var defaultTracer *Tracer

func init() {
	var err error
	defaultTracer, err = NewTracer(tracedir, true)
	if err != nil {
		fmt.Printf("Warning: failed to initialize default tracer: %v\n", err)
	}
}

// NewTracer creates a new Tracer instance and starts the background path checker.
func NewTracer(logDir string, verbose bool) (*Tracer, error) {
	t := &Tracer{
		logDir:   logDir,
		verbose:  verbose,
		stopChan: make(chan struct{}),
	}

	if err := t.autoLoggerFile(); err != nil {
		return nil, err
	}

	go t.schedulePathCheck()
	return t, nil
}

// Close releases resources held by the tracer.
func (t *Tracer) Close() error {
	close(t.stopChan)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.logFile != nil {
		err := t.logFile.Close()
		t.logFile = nil
		return err
	}
	return nil
}

// Trace logs the message and optional error with caller information.
func (t *Tracer) Trace(msg string, x error) string {
	pc := make([]uintptr, 15)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, _ := frames.Next()

	file := filepath.Base(frame.File)
	function := filepath.Base(frame.Function)

	traceln := fmt.Sprintf(placeholder, file, function, frame.Line, msg)

	if x != nil {
		color.Red(traceln)
		t.logX(xflag, traceln, x)
	} else {
		if t.verbose {
			color.Yellow(traceln)
			t.logToFile(lflag, traceln)
		}
	}

	return traceln
}

// Package-level Trace using the default tracer.
func Trace(msg string, x error) string {
	if defaultTracer == nil {
		return ""
	}
	// Use 3 as skip to get the caller of the package-level Trace
	pc := make([]uintptr, 15)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, _ := frames.Next()

	file := filepath.Base(frame.File)
	function := filepath.Base(frame.Function)

	traceln := fmt.Sprintf(placeholder, file, function, frame.Line, msg)

	if x != nil {
		color.Red(traceln)
		defaultTracer.logX(xflag, traceln, x)
	} else {
		if defaultTracer.verbose {
			color.Yellow(traceln)
			defaultTracer.logToFile(lflag, traceln)
		}
	}

	return traceln
}

func (t *Tracer) logX(flag, msg string, x error) {
	logmsg := fmt.Sprintf("%s%s", msg, x)
	t.logToFile(flag, logmsg)
}

func (t *Tracer) logToFile(flag, msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Ensure the file is still valid for the current time
	if err := t.autoLoggerFileLocked(); err != nil {
		fmt.Printf("Could not render log path: %v\n", err)
		return
	}

	if t.xLogger == nil {
		t.xLogger = log.New(t.logFile, "", log.Ldate|log.Ltime|log.Lshortfile)
	}

	t.xLogger.SetPrefix(flag)
	t.xLogger.Println(msg)
}

func (t *Tracer) autoLoggerFile() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.autoLoggerFileLocked()
}

func (t *Tracer) autoLoggerFileLocked() error {
	now := time.Now()
	fpath := fmt.Sprintf(fstamp, t.logDir, now.Year(), now.Month(), now.Day(), now.Hour())

	if fpath == t.loggerPath && t.logFile != nil {
		return nil
	}

	// Rotate or open new file
	if t.logFile != nil {
		t.logFile.Close()
	}

	dir := filepath.Dir(fpath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf(dirX, err)
	}

	file, err := os.OpenFile(fpath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf(fileX, err)
	}

	t.logFile = file
	t.loggerPath = fpath
	t.xLogger = log.New(t.logFile, "", log.Ldate|log.Ltime|log.Lshortfile)

	return nil
}

func (t *Tracer) schedulePathCheck() {
	ticker := time.NewTicker(logInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := t.autoLoggerFile(); err != nil {
				fmt.Printf("Ticker Error: %v\n", err)
			} else {
				// t.Trace("Applied log file update.", nil)
			}
		case <-t.stopChan:
			return
		}
	}
}

// SchedulePathCheck is kept for backward compatibility but is largely redundant now.
func SchedulePathCheck() {
	// The defaultTracer already has its own scheduler.
}

func PrintContextInternals(ctx interface{}, inner bool) {
	contextValues := reflect.ValueOf(ctx).Elem()
	contextKeys := reflect.TypeOf(ctx).Elem()

	if !inner {
		fmt.Printf(fcontext, contextKeys.PkgPath(), contextKeys.Name())
	}

	if contextKeys.Kind() == reflect.Struct {
		for i := 0; i < contextValues.NumField(); i++ {
			reflectValue := contextValues.Field(i)
			// Using reflect.NewAt on unexported fields is still fragile but kept as is for functional parity
			reflectValue = reflect.NewAt(reflectValue.Type(), unsafe.Pointer(reflectValue.UnsafeAddr())).Elem()

			reflectField := contextKeys.Field(i)

			if reflectField.Name == "Context" { // Assuming Context field for recursion
				PrintContextInternals(reflectValue.Interface(), true)
			} else {
				fmt.Printf(fkey, reflectField.Name)
				fmt.Printf(fvalue, reflectValue.Interface())
			}
		}
	} else {
		fmt.Println(xcontext)
	}
}
