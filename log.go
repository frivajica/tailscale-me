// Logging and interactive exit helpers. Everything writes to stdout AND the
// log so a non-technical user can screenshot a failure for diagnosis.

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var logFile *os.File

func ts() string { return time.Now().Format("2006-01-02 15:04:05") }

func logPath() string {
	if logFile == nil {
		return "(unavailable)"
	}
	return logFile.Name()
}

// initLog opens the session log. The path is fixed under the OS temp dir, so
// blow away a pre-planted symlink before opening: the process runs elevated and
// must never write through a link an unprivileged user created ahead of time.
func initLog() error {
	path := filepath.Join(os.TempDir(), "TailscaleMe.log")
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	logFile = f
	return nil
}

// step echoes to the console and appends to the log so a non-technical user
// can screenshot any failure for diagnosis.
func step(format string, a ...interface{}) {
	line := fmt.Sprintf(format, a...)
	fmt.Println(line)
	if logFile != nil {
		logFile.WriteString(ts() + " " + line + "\n")
		logFile.Sync()
	}
}

func fatal(format string, a ...interface{}) {
	step("\nERROR: "+format, a...)
	step("If you need help, send a screenshot (or the file %s) to the setup owner.",
		logPath())
	pauseExit(1)
}

func pauseExit(code int) {
	fmt.Print("\nPress Enter to exit...")
	r := bufio.NewReader(os.Stdin)
	r.ReadString('\n') // EOF-safe: read failure still exits below
	cleanupPrivateTempDir()
	closeLog()
	os.Exit(code)
}

func closeLog() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}
