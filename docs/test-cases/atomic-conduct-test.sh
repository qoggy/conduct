#!/usr/bin/env bash

# Source this file inside one TC's bash heredoc. It captures the real HOME before
# redirecting conduct's store, installs cleanup traps, and fails if the real
# workflows/runs store changes. The caller may register background PIDs with
# conduct_test_register_pid.

conduct_test_snapshot_real_store() {
  local output_file="$1"
  REAL_HOME="$CONDUCT_TEST_REAL_HOME" python3 - "$output_file" <<'PY'
import hashlib
import json
import os
import stat
import sys

real_home = os.environ["REAL_HOME"]
output_file = sys.argv[1]
roots = [".conduct/workflows", ".conduct/runs"]
rows = []

for relative_root in roots:
    root = os.path.join(real_home, relative_root)
    if not os.path.lexists(root):
        rows.append({"path": relative_root, "type": "missing"})
        continue
    paths = [root]
    if os.path.isdir(root) and not os.path.islink(root):
        for directory, directory_names, file_names in os.walk(root):
            directory_names.sort()
            file_names.sort()
            paths.extend(os.path.join(directory, name) for name in directory_names)
            paths.extend(os.path.join(directory, name) for name in file_names)
    for path in sorted(set(paths)):
        metadata = os.lstat(path)
        row = {
            "path": os.path.relpath(path, real_home),
            "mode": stat.S_IMODE(metadata.st_mode),
            "uid": metadata.st_uid,
            "gid": metadata.st_gid,
            "size": metadata.st_size,
            "mtime_ns": metadata.st_mtime_ns,
        }
        if stat.S_ISREG(metadata.st_mode):
            with open(path, "rb") as source:
                row["type"] = "file"
                row["sha256"] = hashlib.sha256(source.read()).hexdigest()
        elif stat.S_ISDIR(metadata.st_mode):
            row["type"] = "directory"
        elif stat.S_ISLNK(metadata.st_mode):
            row["type"] = "symlink"
            row["target"] = os.readlink(path)
        else:
            row["type"] = "other"
        rows.append(row)

with open(output_file, "w", encoding="utf-8") as target:
    json.dump(rows, target, ensure_ascii=False, indent=2, sort_keys=True)
    target.write("\n")
PY
}

conduct_test_register_pid() {
  CONDUCT_TEST_PIDS="${CONDUCT_TEST_PIDS:-} $1"
}

conduct_test_cleanup() {
  local status="$?"
  local pid
  trap - EXIT HUP INT TERM
  for pid in ${CONDUCT_TEST_PIDS:-}; do
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  export HOME="$CONDUCT_TEST_REAL_HOME"
  export PATH="$CONDUCT_TEST_REAL_PATH"
  if ! conduct_test_snapshot_real_store "$CONDUCT_TEST_WORK/real-store-after.json"; then
    echo "FAIL: 无法生成真实 conduct store 的执行后快照" >&2
    status=1
  elif ! diff -u "$CONDUCT_TEST_WORK/real-store-before.json" "$CONDUCT_TEST_WORK/real-store-after.json"; then
    echo "FAIL: 真实 HOME 下 .conduct/workflows 或 .conduct/runs 发生变化" >&2
    status=1
  fi
  rm -rf "$CONDUCT_TEST_WORK"
  exit "$status"
}

conduct_test_setup() {
  : "${CONDUCT:=$PWD/bin/conduct}"
  CONDUCT_TEST_REAL_HOME="${HOME:?HOME 未设置}"
  CONDUCT_TEST_REAL_PATH="${PATH:?PATH 未设置}"
  CONDUCT_TEST_WORK="$(mktemp -d)"
  CONDUCT_TEST_PIDS=""
  export CONDUCT CONDUCT_TEST_REAL_HOME CONDUCT_TEST_REAL_PATH CONDUCT_TEST_WORK
  conduct_test_snapshot_real_store "$CONDUCT_TEST_WORK/real-store-before.json"
  trap conduct_test_cleanup EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  export HOME="$CONDUCT_TEST_WORK"
  mkdir -p "$CONDUCT_TEST_WORK/fakebin"
  export PATH="$CONDUCT_TEST_WORK/fakebin:$CONDUCT_TEST_REAL_PATH"
  WORK="$CONDUCT_TEST_WORK"
  export WORK
}
