#!/usr/bin/env python3
"""A bounded, test-only model comparison using Tutor's existing Codex login.

No production settings are changed. With --dashboard, timing-only records are
also written to the local dashboard; the fixed example text stays in the report.
"""

import argparse
import datetime as dt
import json
import os
import pathlib
import re
import secrets
import shutil
import subprocess
import tempfile
import threading
import time


ROOT = pathlib.Path(__file__).resolve().parents[1]
PROFILES = {
    "sol_low": ("gpt-6.1-sol", "low"),
    "luna_low": ("gpt-6-luna", "low"),
    "luna_none": ("gpt-6-luna", "none"),
}
CASES = [
    ("agreement", "I has a bug.", "I have a bug."),
    (
        "technical_text",
        "Why does `npm test` fails in src/main.go?",
        "Why does `npm test` fail in src/main.go?",
    ),
]


def utc():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def atomic_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary = tempfile.mkstemp(prefix=".benchmark-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as output:
            json.dump(value, output, indent=2)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def run_check(codex, profile, case, instructions, schema, dashboard):
    model, effort = PROFILES[profile]
    name, prompt, expected = case
    start = time.monotonic()
    record = {
        "id": f"{int(time.time()*1000)}-bench-{secrets.token_hex(8)}",
        "started_at": utc(),
        "updated_at": utc(),
        "status": "running",
        "effort": effort,
        "analytics_disabled": True,
        "minimal_instructions": True,
        "prompt_bytes": len(prompt.encode()),
        "corrected_bytes": 0,
        "changed": False,
        "events": [],
    }
    lock = threading.Lock()
    stderr_lines = []
    metric_path = dashboard / (record["id"] + ".json") if dashboard else None

    def save():
        record["updated_at"] = utc()
        if metric_path:
            atomic_json(metric_path, record)

    def mark(name, detail=None, usage=None):
        with lock:
            if not any(event["name"] == name for event in record["events"]):
                event = {"name": name, "at_ms": (time.monotonic() - start) * 1000}
                if detail:
                    event["detail"] = detail
                record["events"].append(event)
            if usage is not None:
                record["usage"] = usage
            save()

    def stdout_reader(stream):
        for line in stream:
            try:
                event = json.loads(line)
            except ValueError:
                continue
            kind = event.get("type")
            if kind in ("thread.started", "turn.started", "turn.completed", "turn.failed"):
                mark(kind, usage=event.get("usage"))
            elif kind in ("item.started", "item.completed"):
                item = event.get("item", {}).get("type")
                if item == "agent_message":
                    mark("model.response_started" if kind == "item.started" else "model.response_completed")
                elif item == "reasoning" and kind == "item.started":
                    mark("model.reasoning_started")

    def stderr_reader(stream):
        for line in stream:
            stderr_lines.append(line.rstrip())
            lowered = line.lower()
            if "catalog" in lowered and any(word in lowered for word in ("timeout", "timed out", "failed")):
                mark("catalog_warning", "Model catalog refresh failed or timed out")
            elif any(word in lowered for word in ("retry", "reconnecting", "stream disconnected")):
                mark("connection_retry", "Codex reported a connection retry")

    mark("prepare_started", f"Benchmark: {model} / {effort}; case: {name}")
    error, corrected, exit_code = None, None, None
    with tempfile.TemporaryDirectory(prefix="english-guard-benchmark-") as directory:
        scratch = pathlib.Path(directory)
        instruction_path = scratch / "instructions.txt"
        schema_path, result_path = scratch / "schema.json", scratch / "result.json"
        instruction_path.write_text(instructions)
        schema_path.write_text(schema)
        args = [
            codex, "exec", "--json", "--disable", "hooks",
            "--disable", "shell_tool", "--disable", "shell_snapshot",
            "--ignore-user-config", "--ignore-rules", "--ephemeral",
            "--skip-git-repo-check", "--sandbox", "read-only", "-C", directory,
            "--model", model,
            "-c", 'approval_policy="never"', "-c", 'web_search="disabled"',
            "-c", "project_doc_max_bytes=0", "-c", "analytics.enabled=false",
            "-c", "model_reasoning_effort=" + json.dumps(effort),
            "-c", "model_instructions_file=" + json.dumps(str(instruction_path)),
            "--output-schema", str(schema_path), "-o", str(result_path),
            json.dumps({"prompt": prompt}, separators=(",", ":")),
        ]
        mark("process_launch")
        try:
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            mark("process_started")
            readers = [threading.Thread(target=stdout_reader, args=(process.stdout,)), threading.Thread(target=stderr_reader, args=(process.stderr,))]
            for reader in readers:
                reader.start()
            try:
                exit_code = process.wait(timeout=35)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                error = "Worker timed out after 35 seconds"
            for reader in readers:
                reader.join()
            mark("process.exited")
            if error is None and exit_code != 0:
                error = "Codex worker exited with code " + str(exit_code)
            if error is None:
                corrected = json.loads(result_path.read_text())["corrected_prompt"]
                if not isinstance(corrected, str):
                    raise ValueError("corrected_prompt was not text")
        except (OSError, ValueError, KeyError) as failure:
            error = str(failure)
        # An API-documented effort can still be unavailable in the local client.
        warnings = [line for line in stderr_lines if "reasoning" in line.lower() and any(word in line.lower() for word in ("unsupported", "not supported", "invalid"))]
        if warnings:
            error = "Requested reasoning effort was rejected or ignored: " + warnings[-1]

    with lock:
        record["status"] = "failed" if error else "ready"
        record["corrected_bytes"] = len(corrected.encode()) if corrected is not None else 0
        record["changed"] = corrected is not None and corrected != prompt
        if error:
            record["error"] = error[:400]
        save()
    mark("failed" if error else "result_ready")
    events = {event["name"]: event["at_ms"] for event in record["events"]}
    def duration(first, last):
        return round((events[last] - events[first]) / 1000, 4) if first in events and last in events else None
    return {
        "profile": profile, "model": model, "effort": effort, "case": name,
        "prompt": prompt, "expected": expected, "corrected": corrected,
        "exact_match": corrected == expected, "error": error,
        "total_s": round((time.monotonic() - start), 4),
        "startup_s": duration("process_started", "turn.started"),
        "request_s": duration("turn.started", "turn.completed"),
        "shutdown_s": duration("turn.completed", "process.exited"),
        "usage": record.get("usage"), "trace_id": record["id"],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=pathlib.Path, required=True)
    parser.add_argument("--dashboard", action="store_true", help="Add timing-only test records to the existing local dashboard")
    options = parser.parse_args()
    codex = shutil.which("codex")
    if not codex:
        parser.error("Codex CLI is not installed")
    source = (ROOT / "codex_tutor.go").read_text()
    instructions = json.loads(re.search(r'const tutorInstructions = ("[^\n]+")', source).group(1))
    schema = re.search(r'const tutorSchema = `([^`]+)`', source).group(1)
    home = pathlib.Path(os.environ.get("CODEX_HOME", str(pathlib.Path.home() / ".codex")))
    dashboard = home / "english-guard" / "metrics" if options.dashboard else None
    results, unavailable = [], set()
    orders = [list(PROFILES), list(reversed(PROFILES))]
    for case, order in zip(CASES, orders):
        for profile in order:
            if profile in unavailable:
                continue
            print(f"Testing {profile}: {case[0]}", flush=True)
            result = run_check(codex, profile, case, instructions, schema, dashboard)
            results.append(result)
            if result["error"]:
                print("  FAILED: " + result["error"], flush=True)
                if "reasoning effort" in result["error"] or "not found" in result["error"]:
                    unavailable.add(profile)
            else:
                print(f"  {result['total_s']:.2f}s total; {result['request_s']}s request; exact correction={result['exact_match']}", flush=True)
            atomic_json(options.output_dir / "results.json", {"created_at": utc(), "results": results})
    print("Results: " + str(options.output_dir / "results.json"), flush=True)


if __name__ == "__main__":
    main()
