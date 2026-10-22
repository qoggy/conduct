#!/usr/bin/env python3
"""Run one self-update acceptance case in an isolated, temporary environment."""

import argparse
import hashlib
import os
from pathlib import Path
import signal
import stat
import subprocess
import tempfile


REPOSITORY = Path(__file__).resolve().parents[3]


def snapshot(real_home):
    result = {}

    def visit(path):
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            result[str(path)] = None
            return
        identity = (metadata.st_mode, metadata.st_uid, metadata.st_gid,
                    metadata.st_size, metadata.st_mtime_ns, metadata.st_ctime_ns)
        if stat.S_ISREG(metadata.st_mode):
            identity += (hashlib.sha256(path.read_bytes()).hexdigest(),)
        elif stat.S_ISLNK(metadata.st_mode):
            identity += (os.readlink(path),)
        result[str(path)] = identity
        if stat.S_ISDIR(metadata.st_mode):
            for child in sorted(path.iterdir()):
                visit(child)

    for relative in ("workflows", "runs", "skill-installations.json", "settings.json"):
        visit(real_home / ".conduct" / relative)
    return result


def isolated_environment(home):
    environment = dict(os.environ)
    environment.pop("CONDUCT_INTERNAL_SKILL_SYNC", None)
    environment.update({
        "HOME": str(home), "USERPROFILE": str(home),
        "XDG_CONFIG_HOME": str(home / ".config"),
        "XDG_DATA_HOME": str(home / ".local/share"),
        "XDG_STATE_HOME": str(home / ".local/state"),
        "XDG_CACHE_HOME": str(home / ".cache"),
        "CODEX_HOME": str(home / ".codex"),
        "CLAUDE_CONFIG_DIR": str(home / ".claude"),
        "LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "en_US.UTF-8",
        "LANG": "en_US.UTF-8",
    })
    return environment


def run(arguments, cwd, environment, expected=0):
    with subprocess.Popen([str(value) for value in arguments], cwd=cwd,
                          env=environment, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, text=True,
                          start_new_session=True) as process:
        try:
            stdout, stderr = process.communicate(timeout=180)
        except BaseException:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass  # The command and its process group have already exited.
            process.communicate()
            raise
    print(stdout, end="")
    print(stderr, end="")
    if process.returncode != expected:
        raise RuntimeError("expected exit {}, got {}: {}".format(
            expected, process.returncode, arguments))
    return stdout + stderr


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def execute_case(case, work):
    home, project = work / "home", work / "project"
    home.mkdir()
    project.mkdir()
    environment = isolated_environment(home)
    binary = work / "conduct"
    if case == "install":
        environment["CONDUCT_INSTALL_DIR"] = str(work / "installed")
        run(["sh", REPOSITORY / "install.sh"], project, environment)
        run([work / "installed/conduct", "version"], project, environment)
        return

    # Build before passing the temporary HOME to product commands, so Go can
    # reuse its normal toolchain and module cache. The binary is disposable.
    build = ["go", "build", "-o", str(binary)]
    if case in ("check", "upgrade"):
        build += ["-ldflags", "-X github.com/qoggy/conduct/internal/cli.version=0.0.0"]
    run(build + ["./cmd/conduct"], REPOSITORY, dict(os.environ))
    if case == "help":
        output = run([binary, "update", "--help"], project, environment)
        for flag in ("--check", "--pre", "--project", "--global"):
            require(flag in output, "missing help flag " + flag)
    elif case == "missing":
        before = hashlib.sha256(binary.read_bytes()).digest()
        output = run([binary, "update", "v99.99.99"], project, environment, expected=1)
        require(bool(output.strip()), "missing failure diagnostic")
        require(hashlib.sha256(binary.read_bytes()).digest() == before,
                "failed update changed executable")
    elif case == "check":
        before = hashlib.sha256(binary.read_bytes()).digest()
        output = run([binary, "update", "--check"], project, environment)
        require("A new conduct version is available" in output, "missing available-version result")
        require(hashlib.sha256(binary.read_bytes()).digest() == before,
                "--check changed executable")
    elif case == "upgrade":
        run([binary, "init", "--agent", "codex,claude-code", "--copy", "--yes"],
            project, environment)
        targets = [project / directory / "skills/conduct"
                   for directory in (".agents", ".claude")]
        for target in targets:
            (target / "obsolete-acceptance-marker").write_text("old copy\n")
        output = run([binary, "update"], project, environment)
        require("conduct updated to " in output and "Skill updated:" in output,
                "CLI replacement and Skill sync must both succeed")
        output = run([binary, "version"], project, environment)
        require(output.startswith("conduct ") and output.strip() != "conduct 0.0.0",
                "executable was not upgraded")
        for target in targets:
            require(not (target / "obsolete-acceptance-marker").exists(),
                    "Skill was not refreshed: " + str(target))
            for relative in ("SKILL.md", "LICENSE", "scripts/workflow_help.py", "scripts/run_help.py"):
                require((target / relative).is_file(), "missing Skill resource " + relative)
    if case != "upgrade":
        require(not (home / ".conduct").exists(), "read-only case wrote metadata")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("case", choices=("help", "missing", "check", "upgrade", "install"))
    arguments = parser.parse_args()
    # Child commands receive environment overrides; the invoking shell is never
    # changed. TemporaryDirectory cleans up success, errors and Ctrl+C alike.
    real_home = Path.home()
    before = snapshot(real_home)
    def interrupted(number, frame):
        raise SystemExit(128 + number)

    previous_handlers = {}
    for number in (signal.SIGHUP, signal.SIGINT, signal.SIGTERM):
        previous_handlers[number] = signal.signal(number, interrupted)
    try:
        with tempfile.TemporaryDirectory(prefix="conduct-self-update-") as directory:
            execute_case(arguments.case, Path(directory))
    finally:
        for number, handler in previous_handlers.items():
            signal.signal(number, handler)
        after = snapshot(real_home)
        changed = sorted(path for path in before.keys() | after.keys()
                         if before.get(path) != after.get(path))
        require(not changed, "real conduct store changed:\n" + "\n".join(changed))
    print("PASS: {}; real conduct store unchanged".format(arguments.case))


if __name__ == "__main__":
    main()
