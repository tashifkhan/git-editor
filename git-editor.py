#!/usr/bin/env python3
"""
Rewrite git history: reset origin URL, squeeze commit dates into a
given interval, change author info, and optionally force-push.
"""

import argparse
import subprocess
import os
import sys
from datetime import datetime, timedelta


def parse_args():
    parser = argparse.ArgumentParser(
        description="Redistribute commit dates and rewrite author info."
    )
    parser.add_argument(
        "--repo-path", default=".", help="Path to the git repository root."
    )
    parser.add_argument("--remote-url", help="New Git remote URL for origin.")
    parser.add_argument(
        "--start-time", help="ISO start timestamp, e.g. 2025-01-01T00:00:00"
    )
    parser.add_argument(
        "--end-time", help="ISO end timestamp,   e.g. 2025-06-30T23:59:59"
    )
    parser.add_argument(
        "--author-name", help="New author name (default: git config user.name)"
    )
    parser.add_argument(
        "--author-email", help="New author email (default: git config user.email)"
    )
    return parser.parse_args()


def expand_and_abs(path: str) -> str:
    # Expand ~ and resolve relative to absolute
    path = os.path.expanduser(path)
    return os.path.abspath(path)


def main():
    args = parse_args()

    # Prompt for missing args
    if not args.remote_url:
        args.remote_url = input("Enter new Git remote URL for origin: ").strip()
    if not args.start_time:
        args.start_time = input(
            "Enter ISO start timestamp (e.g. 2025-01-01T00:00:00): "
        ).strip()
    if not args.end_time:
        args.end_time = input(
            "Enter ISO end timestamp (e.g. 2025-06-30T23:59:59): "
        ).strip()

    # Expand & resolve repo path
    repo = expand_and_abs(args.repo_path)
    if not os.path.isdir(os.path.join(repo, ".git")):
        print(f"Error: {repo} is not a git repo.", file=sys.stderr)
        sys.exit(1)
    os.chdir(repo)

    def git_cfg(key):
        r = subprocess.run(
            ["git", "config", "--get", key], stdout=subprocess.PIPE, text=True
        )
        return r.stdout.strip()

    author = args.author_name or git_cfg("user.name")
    if not author:
        author = input("Enter new author name: ").strip()
    email = args.author_email or git_cfg("user.email")
    if not email:
        email = input("Enter new author email: ").strip()
    if not author or not email:
        print("Error: author name and email must be provided.", file=sys.stderr)
        sys.exit(1)

    # update origin URL
    subprocess.run(["git", "remote", "set-url", "origin", args.remote_url], check=True)

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

    # parse interval
    try:
        st = datetime.fromisoformat(args.start_time)
        et = datetime.fromisoformat(args.end_time)
    except Exception as e:
        print(f"Bad timestamp: {e}", file=sys.stderr)
        sys.exit(1)
    if et < st:
        print("Error: end-time must follow start-time.", file=sys.stderr)
        sys.exit(1)

    # compute per-commit step
    step = (et - st) / (n - 1) if n > 1 else 0

    # write mapping file
    map_name = "commit-date-mapping.txt"
    with open(map_name, "w", encoding="utf-8") as mf:
        for i, h in enumerate(commits):
            if n > 1:
                dt = st + timedelta(seconds=step.total_seconds() * i)
            else:
                dt = st
            ds = dt.strftime("%Y-%m-%d %H:%M:%S +0000")
            mf.write(f"{h} {ds}\n")

    # build env-filter script
    script = (
        f"while read h d t z; do "
        f'if [ "$h" = "$GIT_COMMIT" ]; then '
        f'new_date="$d $t $z"; break; fi; '
        f"done < {map_name}\n"
        'export GIT_AUTHOR_DATE="$new_date"\n'
        'export GIT_COMMITTER_DATE="$new_date"\n'
        f'export GIT_AUTHOR_NAME="{author}"\n'
        f'export GIT_AUTHOR_EMAIL="{email}"\n'
        f'export GIT_COMMITTER_NAME="{author}"\n'
        f'export GIT_COMMITTER_EMAIL="{email}"\n'
    )

    # rewrite history
    subprocess.run(
        ["git", "filter-branch", "--env-filter", script, "--", "--all"], check=True
    )

    # cleanup
    os.remove(map_name)
    subprocess.run(["git", "reflog", "expire", "--expire=now", "--all"], check=True)
    subprocess.run(["git", "gc", "--prune=now", "--aggressive"], check=True)

    # optionally force-push
    choice = input("Do you want to push to origin now? [y/N]: ").strip().lower()
    push_cmd = ["git", "push", "-u", "origin", "--force", "--all"]
    if choice in ("y", "yes"):
        subprocess.run(push_cmd, check=True)
        print("\n\nHistory rewritten and force-pushed.")
    else:
        print("\n\nHistory rewritten—skipping push.")
        print("To push manually, run:")
        print("  " + " ".join(push_cmd))


if __name__ == "__main__":
    main()
