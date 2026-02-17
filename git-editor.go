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
	"strconv"
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

func gitConfig(key string) string {
	out, err := exec.Command("git", "config", "--get", key).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func escapeShellSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "'\"'\"'")
}

func ensureRemote(remoteURL string) {
	if err := exec.Command("git", "remote", "get-url", "origin").Run(); err != nil {
		run("git", "remote", "add", "origin", remoteURL)
		return
	}
	run("git", "remote", "set-url", "origin", remoteURL)
}

func ensureCleanWorktree() {
	out, err := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking git status: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(string(out)) != "" {
		fmt.Fprintln(os.Stderr, "Error: working directory has uncommitted changes. Please commit or stash before rewriting history.")
		os.Exit(1)
	}
}

func prompt(msg string) string {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// cleanInput removes non-printable characters from a string.
func cleanInput(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 32 && r != 127 {
			return r
		}
		return -1
	}, s)
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
	// Handle 'Z' timezone indicator by replacing with '+00:00'
	s = strings.Replace(s, "Z", "+00:00", 1)

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

// parseTimezone parses a timezone offset string in ±HH:MM format
// and returns the offset in seconds and formatted string for git (±HHMM)
func parseTimezone(tzStr string) (int, string, error) {
	if len(tzStr) < 6 {
		return 0, "", fmt.Errorf("timezone must be in format ±HH:MM")
	}

	if tzStr[0] != '+' && tzStr[0] != '-' {
		return 0, "", fmt.Errorf("timezone must start with + or -")
	}

	sign := 1
	if tzStr[0] == '-' {
		sign = -1
	}

	parts := strings.Split(tzStr[1:], ":")
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("timezone must include colon separator (±HH:MM)")
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 || hours > 14 {
		return 0, "", fmt.Errorf("invalid hour value in timezone")
	}

	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, "", fmt.Errorf("invalid minute value in timezone")
	}

	offsetSeconds := sign * (hours*3600 + minutes*60)

	// Format for git: +HHMM or -HHMM (no colon)
	gitFormat := fmt.Sprintf("%s%02d%02d", string(tzStr[0]), hours, minutes)

	return offsetSeconds, gitFormat, nil
}

func main() {
	repoPath := flag.String("repo-path", ".", "Path to git repo")
	remoteURL := flag.String("remote-url", "", "New origin URL")
	startTime := flag.String("start-time", "", "ISO start timestamp")
	endTime := flag.String("end-time", "", "ISO end timestamp")
	authorName := flag.String("author-name", "", "New author name")
	authorEmail := flag.String("author-email", "", "New author email")
	forcePush := flag.Bool("force-push", false, "Force push rewritten history to origin without prompting")
	timezone := flag.String("timezone", "+05:30", "Timezone offset for rewritten commit dates (default: +05:30 for IST). Format: ±HH:MM")
	flag.Parse()

	if *remoteURL == "" {
		*remoteURL = cleanInput(prompt("Enter new Git remote URL for origin: "))
	}

	editDates := false
	if *startTime != "" {
		editDates = true
		if *endTime == "" {
			*endTime = time.Now().UTC().Format(time.RFC3339)
		}
	} else if *endTime != "" {
		fmt.Fprintln(os.Stderr, "Error: --start-time must be provided if --end-time is specified.")
		os.Exit(1)
	} else {
		choice := prompt("Do you want to edit the commit dates? [y/N]: ")
		if strings.HasPrefix(strings.ToLower(choice), "y") {
			editDates = true
			*startTime = cleanInput(prompt("Enter ISO start timestamp (e.g. 2025-01-01T00:00:00): "))
			*endTime = cleanInput(prompt("Enter ISO end timestamp (e.g. 2025-06-30T23:59:59) [optional]: "))
			if *endTime == "" {
				*endTime = time.Now().UTC().Format(time.RFC3339)
			}
		}
	}

	absRepo := expandPath(*repoPath)
	if _, err := os.Stat(filepath.Join(absRepo, ".git")); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s is not a git repo\n", absRepo)
		os.Exit(1)
	}
	os.Chdir(absRepo)
	ensureCleanWorktree()

	if *authorName == "" {
		*authorName = gitConfig("user.name")
	}
	if *authorName == "" {
		*authorName = cleanInput(prompt("Enter new author name: "))
	}
	if *authorEmail == "" {
		*authorEmail = gitConfig("user.email")
	}
	if *authorEmail == "" {
		*authorEmail = cleanInput(prompt("Enter new author email: "))
	}
	if *authorName == "" || *authorEmail == "" {
		fmt.Fprintln(os.Stderr, "Error: author name/email required")
		os.Exit(1)
	}

	// update origin URL (create origin if missing)
	ensureRemote(*remoteURL)

	// list commits
	revList := runOutput("git", "rev-list", "--reverse", "HEAD")
	commits := strings.Fields(revList)
	n := len(commits)
	if n == 0 {
		fmt.Fprintln(os.Stderr, "No commits to rewrite.")
		os.Exit(1)
	}

	// parse times
	var st, et time.Time
	var step time.Duration
	if editDates {
		st = parseTime(*startTime)
		et = parseTime(*endTime)
		if et.Before(st) {
			fmt.Fprintln(os.Stderr, "Error: end-time must come after start-time")
			os.Exit(1)
		}

		if et.Equal(st) && n > 1 {
			fmt.Fprintln(os.Stderr, "Warning: start-time equals end-time. All commits will have the same timestamp.")
			response := prompt("Continue anyway? [y/N]: ")
			if !strings.HasPrefix(strings.ToLower(response), "y") {
				os.Exit(0)
			}
		}

		// compute step
		if n > 1 {
			step = et.Sub(st) / time.Duration(n-1)
		}
	}

	// parse timezone offset (±HH:MM)
	tzOffsetSeconds, tzGitFormat, err := parseTimezone(*timezone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid timezone format '%s': %v\n", *timezone, err)
		fmt.Fprintln(os.Stderr, "Expected format: ±HH:MM (e.g., +05:30, -07:00)")
		os.Exit(1)
	}

	// rewrite history while preserving merge topology
	escapedAuthorName := escapeShellSingleQuote(*authorName)
	escapedAuthorEmail := escapeShellSingleQuote(*authorEmail)
	var envFilterScript strings.Builder
	envFilterScript.WriteString(fmt.Sprintf("GIT_AUTHOR_NAME='%s'\n", escapedAuthorName))
	envFilterScript.WriteString(fmt.Sprintf("GIT_AUTHOR_EMAIL='%s'\n", escapedAuthorEmail))
	envFilterScript.WriteString(fmt.Sprintf("GIT_COMMITTER_NAME='%s'\n", escapedAuthorName))
	envFilterScript.WriteString(fmt.Sprintf("GIT_COMMITTER_EMAIL='%s'\n", escapedAuthorEmail))
	envFilterScript.WriteString("export GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL\n")

	if editDates {
		envFilterScript.WriteString("case \"$GIT_COMMIT\" in\n")
		for i, commitHash := range commits {
			dt := st
			if n > 1 {
				dt = st.Add(step * time.Duration(i))
			}
			// Apply timezone offset to the datetime
			dtWithTz := dt.Add(time.Duration(tzOffsetSeconds) * time.Second)
			// Format with the specified timezone
			ds := dtWithTz.Format("2006-01-02 15:04:05") + " " + tzGitFormat
			escapedDate := escapeShellSingleQuote(ds)
			envFilterScript.WriteString(fmt.Sprintf("  %s)\n", commitHash))
			envFilterScript.WriteString(fmt.Sprintf("    GIT_AUTHOR_DATE='%s'\n", escapedDate))
			envFilterScript.WriteString(fmt.Sprintf("    GIT_COMMITTER_DATE='%s'\n", escapedDate))
			envFilterScript.WriteString("    export GIT_AUTHOR_DATE GIT_COMMITTER_DATE\n")
			envFilterScript.WriteString("    ;;\n")
		}
		envFilterScript.WriteString("esac\n")
	}

	cmd := exec.Command("git", "filter-branch", "-f", "--env-filter", envFilterScript.String(), "--", "--all")
	fmt.Println("Rewriting history...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during history rewrite: %v\n", err)
		fmt.Fprintln(os.Stderr, string(output))
		os.Exit(1)
	}
	if strings.TrimSpace(string(output)) != "" {
		fmt.Println(string(output))
	}

	fmt.Println("History rewritten successfully.")
	// cleanup refs created by filter-branch so aggressive gc can prune old objects.
	refsOutput := runOutput("git", "for-each-ref", "--format=%(refname)", "refs/original/")
	for _, ref := range strings.Fields(refsOutput) {
		run("git", "update-ref", "-d", ref)
	}

	// cleanup
	run("git", "reflog", "expire", "--expire=now", "--all")
	run("git", "gc", "--prune=now", "--aggressive")

	// optionally push
	pushCmd := []string{"git", "push", "-u", "origin", "--force", "--all"}
	if *forcePush {
		run(pushCmd[0], pushCmd[1:]...)
		fmt.Println("\n\nHistory rewritten and force-pushed.")
		return
	}
	choice := prompt("Do you want to push to origin now? [y/N]: ")
	if strings.HasPrefix(strings.ToLower(choice), "y") {
		run(pushCmd[0], pushCmd[1:]...)
		fmt.Println("\n\nHistory rewritten and force-pushed.")
	} else {
		fmt.Println("\n\nHistory rewritten—skipping push.")
		fmt.Println("To push manually, run:")
		fmt.Println("  " + strings.Join(pushCmd, " "))
	}
}
