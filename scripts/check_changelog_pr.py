import re
import subprocess
import sys
from pathlib import Path
from collections import Counter
def unreleased_bullets(content):
    bullets = []
    in_unreleased = False

    for line in content.splitlines():
        stripped = line.strip()

        if stripped == "## [Unreleased]":
            in_unreleased = True
            continue

        if in_unreleased and stripped.startswith("## "):
            break

        if in_unreleased and line.lstrip().startswith("- "):
            bullets.append(line.strip())

    return bullets

def added_bullets(previous, current):
    """Find newly added or modified bullets without flagging reordering."""
    remaining = Counter(previous)
    added = []

    for bullet in current:
        if remaining[bullet] > 0:
            remaining[bullet] -= 1
        else:
            added.append(bullet)

    return added

def validate_bullet(bullet, pr_number):
    match = re.search(r"\((#[0-9]+(?:,\s*#[0-9]+)*)\)\.$", bullet)

    if not match:
        return "missing a trailing PR citation"

    cited_numbers = re.findall(r"#([0-9]+)", match.group(1))

    if str(pr_number) not in cited_numbers:
        return f"must cite PR #{pr_number} in its trailing citation."


def main():
    if len(sys.argv) != 3:
        print(
            "Usage: python3 scripts/check_changelog_pr.py "
            "<base-sha> <pr-number>",
            file=sys.stderr,
        )
        return 2

    base_sha, pr_number = sys.argv[1], sys.argv[2]

    if not pr_number.isdigit() or int(pr_number) < 1:
        print("Error: PR number must be a positive integer.", file=sys.stderr)
        return 2

    try:
        previous_content = subprocess.run(
            ["git", "show", f"{base_sha}:CHANGELOG.md"],
            check=True,
            capture_output=True,
            text=True,
        ).stdout

        current_content = Path("CHANGELOG.md").read_text(encoding="utf-8")
    except (subprocess.CalledProcessError, OSError) as exc:
        print(f"Error reading changelog: {exc}", file=sys.stderr)
        return 2

    previous = unreleased_bullets(previous_content)
    current = unreleased_bullets(current_content)
    new_bullets = added_bullets(previous, current)

    errors = []

    for bullet in new_bullets:
        error = validate_bullet(bullet, pr_number)
        if error:
            errors.append(f"  {bullet}\n    Error: {error}")

    if errors:
        print("CHANGELOG validation failed:")
        print("\n".join(errors))
        return 1

    print(
        f"CHANGELOG validation passed for PR #{pr_number} "
        f"({len(new_bullets)} new bullet(s))."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
