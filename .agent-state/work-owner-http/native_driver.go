// Adapt the accepted PG-only supervisor's fixed command interface to one native
// Work HTTP top. The unchanged supervisor owns process and host-TCP retirement;
// native tests own and join their actual listeners, connections and handlers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const skillOwnerHTTPNativeSelector = `^TestSkillOwnerHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$`

func main() { os.Exit(run()) }

func variableNative(selector string) bool {
	switch selector {
	case "^TestVariableHTTPNativeDeadlines$", "^TestVariableHTTPNativeKeepAliveAndClose$", "^TestVariableHTTPNativeBackpressureAndDisconnect$", "^TestVariableHTTPNative(Deadlines|KeepAliveAndClose|BackpressureAndDisconnect)$":
		return true
	}
	return false
}

func run() int {
	opts := flag.NewFlagSet("work-http-native", flag.ContinueOnError)
	binary := opts.String("test-binary", "", "frozen native race binary")
	selector := opts.String("run", "", "one exact native top")
	directory := opts.String("directory", "", "new task-owned directory")
	if opts.Parse(os.Args[1:]) != nil || opts.NArg() != 0 || !filepath.IsAbs(*binary) || !filepath.IsAbs(*directory) || (!regexp.MustCompile(`^\^TestWorkHTTPNative(?:Deadlines|KeepaliveAndEOF|WriteCloseAndConfirmationTail)\$$`).MatchString(*selector) && !variableNative(*selector) && *selector != skillOwnerHTTPNativeSelector) {
		fmt.Fprintln(os.Stderr, "STOP exact native binary, selector and directory required")
		return 1
	}
	if err := os.Mkdir(*directory, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "STOP runtime directory must be new")
		return 1
	}
	tmp := filepath.Join(*directory, "tmp")
	if err := os.Mkdir(tmp, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "STOP private temporary directory unavailable")
		return 1
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 105*time.Second)
	defer cancel()
	started := time.Now()
	cmd := exec.CommandContext(ctx, *binary, "-test.v", "-test.count=1", "-test.timeout=90s", "-test.run="+*selector)
	nativeGate := "AGENTEAM_WORK_OWNER_HTTP_NATIVE"
	if variableNative(*selector) {
		nativeGate = "AGENTEAM_PROJECT_VARIABLE_HTTP_NATIVE"
	} else if *selector == skillOwnerHTTPNativeSelector {
		nativeGate = "AGENTEAM_SKILL_HTTP_NATIVE"
	}
	// Copy inherited fixed toolchain/cache inputs, replacing only this task's
	// explicit native gate and its fresh private temporary directory.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != nativeGate && key != "TMPDIR" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, nativeGate+"=1", "TMPDIR="+tmp)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "STOP native child failed to start")
		_ = os.Remove(tmp)
		return 1
	}
	record, _ := json.Marshal(map[string]any{"kind": "work-http-native", "child_pid": cmd.Process.Pid})
	manifestErr := os.WriteFile(filepath.Join(*directory, "owned.json"), record, 0600)
	fmt.Printf("CHILD pid=%d selector=%s kind=native-http\n", cmd.Process.Pid, *selector)
	err := cmd.Wait()
	code := 0
	if err != nil {
		code = 1
		var status *exec.ExitError
		if errors.As(err, &status) && status.ExitCode() > 0 {
			code = status.ExitCode()
		}
	}
	if ctx.Err() != nil || manifestErr != nil {
		code = 1
	}
	fmt.Printf("CHILD actual_wait pid=%d state=%v\n", cmd.Process.Pid, cmd.ProcessState)
	entries, readErr := os.ReadDir(tmp)
	empty := readErr == nil && len(entries) == 0
	if !empty {
		code = 1
	}
	fmt.Printf("NATIVE runtime_empty=%t actual_child_wait=true\n", empty)
	// Remove only the directory created by this invocation after actual Wait.
	// A failed empty check remains failure even when post-run cleanup succeeds.
	removed := os.RemoveAll(tmp) == nil
	if !removed {
		code = 1
	}
	fmt.Printf("DRIVER terminal exit=%d elapsed=%.3fs child_started=true actual_child_wait=true private_removed=%t\n", code, time.Since(started).Seconds(), removed)
	return code
}
