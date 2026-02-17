#!/usr/bin/env python3
"""
Rewrite git history: reset origin URL, squeeze commit dates into a
given interval, change author info, and optionally force-push.
"""

import argparse
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta


def parse_args():
    parser = argparse.ArgumentParser(
        description="Redistribute commit dates and rewrite author info."
    )
    parser.add_argument(
        "--repo-path",
        default=".",
        help="Path to the git repository root.",
    )
    parser.add_argument(
        "--remote-url",
        help="New Git remote URL for origin.",
    )
    parser.add_argument(
        "--start-time",
        help="ISO start timestamp, e.g. 2025-01-01T00:00:00",
    )
    parser.add_argument(
        "--end-time",
        help="ISO end timestamp,   e.g. 2025-06-30T23:59:59",
    )
    parser.add_argument(
        "--author-name",
        help="New author name (default: git config user.name)",
    )
    parser.add_argument(
        "--author-email",
        help="New author email (default: git config user.email)",
    )
    parser.add_argument(
        "--force-push",
        action="store_true",
        help="Force push rewritten history to origin without prompting",
    )
    parser.add_argument(
        "--timezone",
        default="+05:30",
        help="Timezone offset for rewritten commit dates (default: +05:30 for IST). Format: ±HH:MM",
    )
    return parser.parse_args()


def clean_input(s: str) -> str:
    """Remove non-printable characters from a string."""
    return "".join(ch for ch in s if ch >= " " and ch != "\x7f")


def git_config(key: str) -> str:
    r = subprocess.run(
        ["git", "config", "--get", key], stdout=subprocess.PIPE, text=True
    )
    return r.stdout.strip() if r.returncode == 0 else ""


def escape_shell_single_quote(s: str) -> str:
    return s.replace("'", "'\"'\"'")


def ensure_remote(remote_url: str) -> None:
    if subprocess.run(["git", "remote", "get-url", "origin"]).returncode != 0:
        subprocess.run(["git", "remote", "add", "origin", remote_url], check=True)
    else:
        subprocess.run(["git", "remote", "set-url", "origin", remote_url], check=True)


def ensure_clean_worktree() -> None:
    r = subprocess.run(
        ["git", "status", "--porcelain"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if r.returncode != 0:
        print(f"Error checking git status: {r.stderr}", file=sys.stderr)
        sys.exit(1)
    if r.stdout.strip():
        print(
            "Error: working directory has uncommitted changes. Please commit or stash before rewriting history.",
            file=sys.stderr,
        )
        sys.exit(1)


def expand_and_abs(path: str) -> str:
    """Expand ~ and resolve relative to absolute"""
    path = os.path.expanduser(path)
    return os.path.abspath(path)


def main():
    args = parse_args()

    if not args.remote_url:
        args.remote_url = clean_input(
            input("Enter new Git remote URL for origin: ").strip()
        )

    edit_dates = False

    if args.start_time:
        edit_dates = True
        if not args.end_time:
            args.end_time = datetime.now().isoformat()

    elif args.end_time:
        print(
            "Error: --start-time must be provided if --end-time is specified.",
            file=sys.stderr,
        )
        sys.exit(1)

    else:
        choice = input("Do you want to edit the commit dates? [y/N]: ").strip().lower()

        if choice in ("y", "yes"):
            edit_dates = True

            args.start_time = clean_input(
                input("Enter ISO start timestamp (e.g. 2025-01-01T00:00:00): ").strip()
            )

            args.end_time = clean_input(
                input(
                    "Enter ISO end timestamp (e.g. 2025-06-30T23:59:59) [optional]: "
                ).strip()
            )

            if not args.end_time:
                args.end_time = datetime.now().isoformat()

    repo = expand_and_abs(args.repo_path)

    if not os.path.isdir(os.path.join(repo, ".git")):
        print(f"Error: {repo} is not a git repo.", file=sys.stderr)
        sys.exit(1)

    os.chdir(repo)
    ensure_clean_worktree()

    author = args.author_name or git_config("user.name")

    if not author:
        author = clean_input(input("Enter new author name: ").strip())

    email = args.author_email or git_config("user.email")

    if not email:
        email = clean_input(input("Enter new author email: ").strip())

    if not author or not email:
        print("Error: author name and email must be provided.", file=sys.stderr)
        sys.exit(1)

    ensure_remote(args.remote_url)

    # list all commits oldest→newest
    rev = subprocess.run(
        ["git", "rev-list", "--reverse", "HEAD"],
        stdout=subprocess.PIPE,
        text=True,
        check=True,
    )

    commits = rev.stdout.strip().splitlines()

    n = len(commits)

    if n == 0:
        print("No commits to rewrite.", file=sys.stderr)
        sys.exit(1)

    st = et = None
    step = timedelta()
    if edit_dates:
        # parse interval
        try:
            # Handle 'Z' timezone indicator by replacing with '+00:00'
            start_str = args.start_time.replace("Z", "+00:00")
            end_str = args.end_time.replace("Z", "+00:00")
            st = datetime.fromisoformat(start_str)
            et = datetime.fromisoformat(end_str)

        except Exception as e:
            print(f"Bad timestamp: {e}", file=sys.stderr)
            sys.exit(1)

        if et < st:
            print("Error: end-time must follow start-time.", file=sys.stderr)
            sys.exit(1)

        if et == st and n > 1:
            print(
                "Warning: start-time equals end-time. All commits will have the same timestamp.",
                file=sys.stderr,
            )
            response = input("Continue anyway? [y/N]: ").strip().lower()
            if response not in ("y", "yes"):
                sys.exit(0)

        # compute per-commit step
        step = (et - st) / (n - 1) if n > 1 else timedelta()

    # parse timezone offset (±HH:MM)
    try:
        tz_str = args.timezone
        if not tz_str.startswith(("+", "-")):
            raise ValueError("Timezone must start with + or -")
        sign = 1 if tz_str.startswith("+") else -1
        time_part = tz_str[1:]
        if ":" not in time_part:
            raise ValueError("Timezone must include colon separator")
        hh, mm = time_part.split(":")
        tz_offset_hours = int(hh)
        tz_offset_minutes = int(mm)
        if (
            tz_offset_hours < 0
            or tz_offset_hours > 14
            or tz_offset_minutes < 0
            or tz_offset_minutes > 59
        ):
            raise ValueError("Invalid hour or minute values")
        tz_delta = timedelta(hours=tz_offset_hours, minutes=tz_offset_minutes) * sign
        # Format for git: +HHMM or -HHMM (no colon)
        tz_git_format = f"{'+' if sign == 1 else '-'}{hh.zfill(2)}{mm.zfill(2)}"
    except Exception as e:
        print(f"Error: invalid timezone format '{args.timezone}': {e}", file=sys.stderr)
        print("Expected format: ±HH:MM (e.g., +05:30, -07:00)", file=sys.stderr)
        sys.exit(1)

    # rewrite history using rebase
    escaped_author = escape_shell_single_quote(author)
    escaped_email = escape_shell_single_quote(email)
    rebase_script_parts = []
    for i, commit_hash in enumerate(commits):
        rebase_script_parts.append(f"pick {commit_hash}")

        date_cmd_part = ""
        if edit_dates:
            if st is not None:
                dt = st + step * i if n > 1 else st
                # Apply timezone offset to the datetime
                dt_with_tz = dt + tz_delta
                # Format with the specified timezone
                ds = dt_with_tz.strftime(f"%Y-%m-%d %H:%M:%S {tz_git_format}")
                date_cmd_part = f"GIT_COMMITTER_DATE='{ds}' GIT_AUTHOR_DATE='{ds}' "

        rebase_script_parts.append(
            f"exec GIT_COMMITTER_NAME='{escaped_author}' GIT_COMMITTER_EMAIL='{escaped_email}' {date_cmd_part}git commit --amend --no-edit --author='{escaped_author} <{escaped_email}>'"
        )
    rebase_script = "\n".join(rebase_script_parts) + "\n"

    # rewriting the full history of the current branch by using `rebase -i --root`
    rebase_cmd = ["git", "rebase", "-i", "--root"]

    # using tempo file to pass the script to the rebase command
    editor_script_content = f"#!/bin/sh\ncat <<'EOF' > \"$1\"\n{rebase_script}EOF\n"

    with tempfile.NamedTemporaryFile(
        mode="w", delete=False, suffix=".sh"
    ) as editor_script_file:
        editor_script_file.write(editor_script_content)
        editor_script_path = editor_script_file.name

    os.chmod(editor_script_path, 0o755)

    env = os.environ.copy()
    env["GIT_SEQUENCE_EDITOR"] = editor_script_path

    print("Rewriting history...")
    rebase_proc = subprocess.run(
        rebase_cmd,
        env=env,
        capture_output=True,
        text=True,
    )

    os.remove(editor_script_path)

    if rebase_proc.returncode != 0:
        print("Error during rebase:", file=sys.stderr)
        print(rebase_proc.stdout, file=sys.stderr)
        print(rebase_proc.stderr, file=sys.stderr)
        sys.exit(1)

    print("History rewritten successfully.")

    # cleanup
    subprocess.run(["git", "reflog", "expire", "--expire=now", "--all"], check=True)
    subprocess.run(["git", "gc", "--prune=now", "--aggressive"], check=True)

    push_cmd = ["git", "push", "-u", "origin", "--force", "--all"]
    if args.force_push:
        subprocess.run(push_cmd, check=True)
        print("\n\nHistory rewritten and force-pushed.")
        return

    # force-push
    choice = input("Do you want to push to origin now? [y/N]: ").strip().lower()
    if choice in ("y", "yes"):
        subprocess.run(push_cmd, check=True)
        print("\n\nHistory rewritten and force-pushed.")
    else:
        print("\n\nHistory rewritten—skipping push.")
        print("To push manually, run:")
        print("  " + " ".join(push_cmd))


if __name__ == "__main__":
    main()
