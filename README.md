# Git Editor Utility

This repository provides a utility to rewrite Git history in bulk, allowing you to:

- Change the remote origin URL
- Redistribute commit dates into a specified interval
- Change author name and email for all commits
- Optionally force-push the rewritten history to the remote

There are two implementations:

- `git-editor.go` (Go)
- `git-editor.py` (Python 3)

Both scripts perform identical operations and have the same command-line interfaces.

## Features

- **Bulk rewrite of commit dates**: Squeeze all commit timestamps into a user-specified interval.
- **Change author info**: Set a new author name and email for all commits.
- **Change remote URL**: Update the `origin` remote to a new URL.
- **Timezone support**: Specify timezone for commit dates (default: IST +05:30).
- **Force-push option**: Optionally force-push the rewritten history to the new remote.
- **Safety checks**: Prompts for missing arguments and validates repository state.

## Usage

### Prerequisites

- For `git-editor.go`: Go 1.18+ (or compatible)
- For `git-editor.py`: Python 3.7+
- A clean working directory (no uncommitted changes)
- A backup of your repository (rewriting history is destructive!)

### 1. Build or use the script

#### Go version

```sh
# Build
$ go build -o git-editor git-editor.go
# Run
$ ./git-editor --help
$ ./git-editor [flags]
```

#### Python version

```sh
$ python3 git-editor.py --help
$ python3 git-editor.py [flags]
```

### 2. Command-line Flags

| Flag           | Description                                             |
| -------------- | ------------------------------------------------------- |
| --repo-path    | Path to the git repo (default: ".")                     |
| --remote-url   | New origin URL                                          |
| --start-time   | ISO start timestamp (e.g. 2025-01-01T00:00:00)          |
| --end-time     | ISO end timestamp (e.g. 2025-06-30T23:59:59)            |
| --author-name  | New author name                                         |
| --author-email | New author email                                        |
| --timezone     | Timezone offset (default: +05:30 IST). Format: ±HH:MM  |

If any flag is omitted, you will be prompted interactively.

### 3. Examples

#### Go Version

```sh
# Example: Rewrite history for a repo with IST timezone (default)
$ ./git-editor \
    --repo-path ~/my-repo \
    --remote-url git@github.com:me/new-repo.git \
    --start-time 2025-01-01T00:00:00 \
    --end-time 2025-06-30T23:59:59 \
    --author-name "Alice Example" \
    --author-email "alice@example.com"
```

#### Python Version

```sh
# Example: Rewrite history with a custom timezone (PST)
$ python3 git-editor.py \
    --repo-path ~/my-repo \
    --remote-url git@github.com:me/new-repo.git \
    --start-time 2025-01-01T00:00:00 \
    --end-time 2025-06-30T23:59:59 \
    --timezone "-08:00" \
    --author-name "Alice Example" \
    --author-email "alice@example.com"
```

#### Both Versions Support All Features

```sh
# Example: Rewrite history with UTC timezone (Go)
$ ./git-editor \
    --timezone "+00:00" \
    --start-time 2025-01-01T00:00:00Z \
    --end-time 2025-06-30T23:59:59Z \
    --author-name "Bob Example" \
    --author-email "bob@example.com"

# Example: Rewrite history with UTC timezone (Python)
$ python3 git-editor.py \
    --timezone "+00:00" \
    --start-time 2025-01-01T00:00:00Z \
    --end-time 2025-06-30T23:59:59Z \
    --author-name "Bob Example" \
    --author-email "bob@example.com"
```

You will be prompted to confirm the force-push at the end.

> **Note:** For more detailed examples including working hours, timezone references, and troubleshooting, see [EXAMPLES.md](EXAMPLES.md).

## How it Works

1. **Update origin URL**: Sets the `origin` remote to the new URL.
2. **List all commits**: Gets all commit hashes in chronological order (oldest to newest).
3. **Redistribute dates**: Evenly spaces commit dates between the start and end times.
4. **Apply timezone**: Converts commit timestamps to the specified timezone (default: IST +05:30).
5. **Rewrite history**: Uses `git rebase` to update commit dates and author info.
6. **Cleanup**: Removes reflog entries and runs `git gc --aggressive`.
7. **Force-push**: Optionally pushes the rewritten history to the remote.

## Safety Notes

- **This operation rewrites history and is destructive.**
- Always make a backup of your repository before running this tool.
- All branches and tags will be rewritten.
- You will need to force-push (`--force --all`) to update the remote.
- Collaborators will need to re-clone or reset their local copies.

## License

See [LICENSE](LICENSE).
