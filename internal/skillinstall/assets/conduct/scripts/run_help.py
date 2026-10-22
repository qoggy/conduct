#!/usr/bin/env python3
"""Output live conduct workflow run help in XML format in one pass."""

import subprocess
import sys


HELP_COMMANDS = (
    "conduct help workflow run",
    "conduct help run-prompts",
    "conduct help run",
    "conduct help run show",
    "conduct help run list",
    "conduct help run wait",
    "conduct help run resume",
    "conduct help run stop",
)


def write_output(stdout: str, stderr: str) -> None:
    # XML-style tags only help AI distinguish command blocks; keep the body unchanged instead of escaping it as strict XML.
    output = stdout
    if stderr:
        if output and not output.endswith("\n"):
            output += "\n"
        output += stderr
    sys.stdout.write(output)
    if output and not output.endswith("\n"):
        print()


def main() -> int:
    exit_code = 0
    for command in HELP_COMMANDS:
        result = subprocess.run(
            ["bash", "-lc", command],
            capture_output=True,
            check=False,
            text=True,
        )
        print(
            f'<command invocation="{command}" exit-code="{result.returncode}">'
        )
        write_output(result.stdout, result.stderr)
        print("</command>")
        if exit_code == 0 and result.returncode != 0:
            exit_code = result.returncode
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
