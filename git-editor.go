// rewrite_history.go
//
// Rewrite git history: reset origin URL, squeeze commit dates into a
// given interval, change author info, and optionally force-push.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running %s %v: %v\n", name, args, err)
		os.Exit(1)
	}
}

func runOutput(name string, args ...string) string {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Error running %s %v: %v\nOutput: %s\n",
			name, args, err, string(out))
		os.Exit(1)
	}
	return string(out)
}

func prompt(msg string) string {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// expandPath handles ~ and returns an absolute path
func expandPath(p string) string {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Cannot get home directory:", err)
			os.Exit(1)
		}
		p = filepath.Join(home, p[1:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid path:", err)
		os.Exit(1)
	}
	return abs
}

func parseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	layout := "2006-01-02T15:04:05"
	t, err := time.ParseInLocation(layout, s, time.UTC)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid timestamp %q: %v\n", s, err)
		os.Exit(1)
	}
	return t.UTC()
}

func main() {
	repoPath := flag.String("repo-path", ".", "Path to git repo")
	remoteURL := flag.String("remote-url", "", "New origin URL")
	startTime := flag.String("start-time", "", "ISO start timestamp")
	endTime := flag.String("end-time", "", "ISO end timestamp")
	authorName := flag.String("author-name", "", "New author name")
	authorEmail := flag.String("author-email", "", "New author email")
	flag.Parse()

	if *remoteURL == "" {
		*remoteURL = prompt("Enter new Git remote URL for origin: ")
	}
	if *startTime == "" {
		*startTime = prompt(
			"Enter ISO start timestamp (e.g. 2025-01-01T00:00:00): ")
	}
	if *endTime == "" {
		*endTime = prompt(
			"Enter ISO end timestamp (e.g. 2025-06-30T23:59:59): ")
	}

	absRepo := expandPath(*repoPath)
	if _, err := os.Stat(filepath.Join(absRepo, ".git")); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s is not a git repo\n", absRepo)
		os.Exit(1)
	}
	os.Chdir(absRepo)

	cfg := func(key string) string {
		return strings.TrimSpace(
			runOutput("git", "config", "--get", key),
		)
	}
	if *authorName == "" {
		*authorName = cfg("user.name")
	}
	if *authorName == "" {
		*authorName = prompt("Enter new author name: ")
	}
	if *authorEmail == "" {
		*authorEmail = cfg("user.email")
	}
	if *authorEmail == "" {
		*authorEmail = prompt("Enter new author email: ")
	}
	if *authorName == "" || *authorEmail == "" {
		fmt.Fprintln(os.Stderr, "Error: author name/email required")
		os.Exit(1)
	}

	// update origin URL
	run("git", "remote", "set-url", "origin", *remoteURL)

	// list commits
	revList := runOutput("git", "rev-list", "--reverse", "HEAD")
	commits := strings.Fields(revList)
	n := len(commits)
	if n == 0 {
		fmt.Fprintln(os.Stderr, "No commits to rewrite.")
		os.Exit(1)
	}

	// parse times
	st := parseTime(*startTime)
	et := parseTime(*endTime)
	if et.Before(st) {
		fmt.Fprintln(os.Stderr, "Error: end-time must come after start-time")
		os.Exit(1)
	}

	// compute step
	var step time.Duration
	if n > 1 {
		step = et.Sub(st) / time.Duration(n-1)
	}

	// rewrite history using rebase
	var rebaseScript strings.Builder
	for i, commitHash := range commits {
		dt := st
		if n > 1 {
			dt = st.Add(step * time.Duration(i))
		}
		ds := dt.Format("2006-01-02 15:04:05 +0000")

		rebaseScript.WriteString(fmt.Sprintf("pick %s\n", commitHash))
		rebaseScript.WriteString(fmt.Sprintf(
			"exec GIT_COMMITTER_DATE='%s' GIT_AUTHOR_DATE='%s' git commit --amend --no-edit --author='%s <%s>'\n",
			ds, ds, *authorName, *authorEmail,
		))
	}

	// We are rewriting the full history of the current branch, so we use `rebase -i --root`.
	rebaseCmd := []string{"rebase", "-i", "--root"}

	// Use a temporary file to pass the script to the rebase command.
	editorScriptFile, err := os.CreateTemp("", "git-editor-*.sh")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create temp file for editor script:", err)
		os.Exit(1)
	}
	defer os.Remove(editorScriptFile.Name())

	editorScriptContent := fmt.Sprintf("#!/bin/sh\ncat <<'EOF' > \"$1\"\n%sEOF\n", rebaseScript.String())
	if _, err := editorScriptFile.WriteString(editorScriptContent); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to write to editor script:", err)
		editorScriptFile.Close()
		os.Exit(1)
	}
	if err := editorScriptFile.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to close editor script:", err)
		os.Exit(1)
	}

	if err := os.Chmod(editorScriptFile.Name(), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to make editor script executable:", err)
		os.Exit(1)
	}

	cmd := exec.Command("git", rebaseCmd...)
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR="+editorScriptFile.Name())

	fmt.Println("Rewriting history...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during rebase: %v\n", err)
		fmt.Fprintln(os.Stderr, string(output))
		os.Exit(1)
	}

	fmt.Println("History rewritten successfully.")

	// cleanup
	run("git", "reflog", "expire", "--expire=now", "--all")
	run("git", "gc", "--prune=now", "--aggressive")

	// optionally push
	choice := prompt("Do you want to push to origin now? [y/N]: ")
	pushCmd := []string{"git", "push", "-u", "origin", "--force", "--all"}
	if strings.HasPrefix(strings.ToLower(choice), "y") {
		run(pushCmd[0], pushCmd[1:]...)
		fmt.Println("\n\nHistory rewritten and force-pushed.")
	} else {
		fmt.Println("\n\nHistory rewritten—skipping push.")
		fmt.Println("To push manually, run:")
		fmt.Println("  " + strings.Join(pushCmd, " "))
	}
}
