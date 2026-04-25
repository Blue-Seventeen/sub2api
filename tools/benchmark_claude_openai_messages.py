#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import math
import os
import sys
import time
import urllib.error
import urllib.request
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


DEFAULT_INSTANCES = (
    ("live_18080", "http://127.0.0.1:18080"),
    ("official_fresh_18081", "http://127.0.0.1:18081"),
    ("current_fresh_18082", "http://127.0.0.1:18082"),
)

DEFAULT_MODEL = "claude-opus-4-6"
DEFAULT_PROMPT = 'Reply with exactly "benchmark-ok".'
DEFAULT_TIMEOUT_SECONDS = 180.0
DEFAULT_ROUNDS = 20

CLAUDE_CODE_HEADERS = {
    "Content-Type": "application/json",
    "anthropic-version": "2023-06-01",
    "anthropic-beta": (
        "claude-code-20250219,"
        "interleaved-thinking-2025-05-14,"
        "fine-grained-tool-streaming-2025-05-14"
    ),
    "User-Agent": "claude-cli/2.1.22 (external, cli)",
    "X-Stainless-Lang": "js",
    "X-Stainless-Package-Version": "0.70.0",
    "X-Stainless-OS": "Linux",
    "X-Stainless-Arch": "arm64",
    "X-Stainless-Runtime": "node",
    "X-Stainless-Runtime-Version": "v24.13.0",
    "X-Stainless-Retry-Count": "0",
    "X-Stainless-Timeout": "600",
    "X-App": "cli",
    "Anthropic-Dangerous-Direct-Browser-Access": "true",
}

FIXED_METADATA_USER_ID = (
    "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    "_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)


@dataclass(frozen=True)
class InstanceTarget:
    name: str
    base_url: str

    @property
    def request_url(self) -> str:
        return self.base_url.rstrip("/") + "/v1/messages"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Benchmark Claude Code -> OpenAI group -> /v1/messages across multiple "
            "Sub2API instances with a fixed Claude Code style request."
        )
    )
    parser.add_argument(
        "--api-key",
        default=os.environ.get("SUB2API_BENCH_API_KEY", "").strip(),
        help=(
            "Gateway API key. Defaults to env SUB2API_BENCH_API_KEY. "
            "This is sent via x-api-key."
        ),
    )
    parser.add_argument(
        "--rounds",
        type=int,
        default=DEFAULT_ROUNDS,
        help=f"Number of rounds per instance. Default: {DEFAULT_ROUNDS}",
    )
    parser.add_argument(
        "--order",
        choices=("alternating", "grouped"),
        default="alternating",
        help="Request scheduling order. Default: alternating",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=DEFAULT_TIMEOUT_SECONDS,
        help=f"Per-request timeout in seconds. Default: {DEFAULT_TIMEOUT_SECONDS}",
    )
    parser.add_argument(
        "--model",
        default=DEFAULT_MODEL,
        help=f"Anthropic-side model id used in the fixed request body. Default: {DEFAULT_MODEL}",
    )
    parser.add_argument(
        "--prompt",
        default=DEFAULT_PROMPT,
        help="Prompt text used in the fixed request body.",
    )
    parser.add_argument(
        "--instance",
        action="append",
        default=[],
        metavar="NAME=URL",
        help=(
            "Override instances. Can be repeated. "
            "Default: live_18080/official_fresh_18081/current_fresh_18082"
        ),
    )
    parser.add_argument(
        "--output-prefix",
        default="claude_openai_messages_benchmark",
        help="Prefix for JSONL / summary filenames under test-results.",
    )
    return parser.parse_args()


def ensure_api_key(api_key: str) -> str:
    api_key = api_key.strip()
    if api_key:
        return api_key
    raise SystemExit(
        "Missing API key. Pass --api-key or set SUB2API_BENCH_API_KEY."
    )


def parse_instances(raw_instances: list[str]) -> list[InstanceTarget]:
    if not raw_instances:
        return [InstanceTarget(name=name, base_url=url) for name, url in DEFAULT_INSTANCES]

    instances: list[InstanceTarget] = []
    seen_names: set[str] = set()
    for raw in raw_instances:
        if "=" not in raw:
            raise SystemExit(f"Invalid --instance value: {raw!r}. Expected NAME=URL.")
        name, url = raw.split("=", 1)
        name = name.strip()
        url = url.strip().rstrip("/")
        if not name or not url:
            raise SystemExit(f"Invalid --instance value: {raw!r}. Expected NAME=URL.")
        if name in seen_names:
            raise SystemExit(f"Duplicate instance name: {name!r}")
        seen_names.add(name)
        instances.append(InstanceTarget(name=name, base_url=url))
    return instances


def build_request_body(model: str, prompt: str) -> bytes:
    payload = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "text",
                        "text": prompt,
                        "cache_control": {"type": "ephemeral"},
                    }
                ],
            }
        ],
        "system": [
            {
                "type": "text",
                "text": "You are Claude Code, Anthropic's official CLI for Claude.",
                "cache_control": {"type": "ephemeral"},
            }
        ],
        "metadata": {"user_id": FIXED_METADATA_USER_ID},
        "max_tokens": 128,
        "temperature": 0,
        "stream": True,
    }
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def build_headers(api_key: str) -> dict[str, str]:
    headers = dict(CLAUDE_CODE_HEADERS)
    headers["x-api-key"] = api_key
    return headers


def iter_schedule(
    instances: list[InstanceTarget],
    rounds: int,
    order: str,
) -> list[tuple[int, InstanceTarget]]:
    schedule: list[tuple[int, InstanceTarget]] = []
    if order == "grouped":
        for instance in instances:
            for round_index in range(1, rounds + 1):
                schedule.append((round_index, instance))
        return schedule

    for round_index in range(1, rounds + 1):
        for instance in instances:
            schedule.append((round_index, instance))
    return schedule


def iso_now() -> str:
    return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")


def truncate_text(value: str, limit: int = 240) -> str:
    if len(value) <= limit:
        return value
    return value[: limit - 3] + "..."


def extract_event_type_from_data_line(payload: str) -> str | None:
    payload = payload.strip()
    if not payload or payload == "[DONE]":
        return None
    try:
        obj = json.loads(payload)
    except json.JSONDecodeError:
        return None
    value = obj.get("type")
    if isinstance(value, str) and value.strip():
        return value.strip()
    return None


def make_request(
    instance: InstanceTarget,
    body: bytes,
    headers: dict[str, str],
    timeout: float,
    seq: int,
    round_index: int,
) -> dict[str, Any]:
    result: dict[str, Any] = {
        "seq": seq,
        "round": round_index,
        "instance_name": instance.name,
        "base_url": instance.base_url,
        "request_url": instance.request_url,
        "started_at": iso_now(),
        "status": None,
        "first_line_s": None,
        "first_data_s": None,
        "first_delta_s": None,
        "elapsed_s": None,
        "lines_read": 0,
        "data_lines": 0,
        "bytes_read": 0,
        "content_type": "",
        "first_line_preview": None,
        "first_data_preview": None,
        "first_delta_preview": None,
        "first_delta_type": None,
        "error": None,
        "error_body_preview": None,
    }

    start = time.perf_counter()
    request = urllib.request.Request(
        url=instance.request_url,
        data=body,
        headers=headers,
        method="POST",
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            result["status"] = getattr(response, "status", None)
            result["content_type"] = response.headers.get("Content-Type", "")
            while True:
                line_bytes = response.readline()
                if not line_bytes:
                    break
                now = time.perf_counter()
                result["lines_read"] += 1
                result["bytes_read"] += len(line_bytes)

                line = line_bytes.rstrip(b"\r\n")
                if not line:
                    continue
                line_text = line.decode("utf-8", errors="replace")

                if result["first_line_s"] is None:
                    result["first_line_s"] = round(now - start, 6)
                    result["first_line_preview"] = truncate_text(line_text)

                if line_text.startswith("data:"):
                    payload = line_text[5:].lstrip(" \t")
                    result["data_lines"] += 1
                    if result["first_data_s"] is None:
                        result["first_data_s"] = round(now - start, 6)
                        result["first_data_preview"] = truncate_text(payload)

                    event_type = extract_event_type_from_data_line(payload)
                    if event_type and result["first_delta_s"] is None and event_type == "content_block_delta":
                        result["first_delta_s"] = round(now - start, 6)
                        result["first_delta_type"] = event_type
                        result["first_delta_preview"] = truncate_text(payload)
    except urllib.error.HTTPError as exc:
        result["status"] = exc.code
        result["content_type"] = exc.headers.get("Content-Type", "") if exc.headers else ""
        try:
            error_body = exc.read()
        except Exception as read_exc:  # pragma: no cover - defensive
            result["error"] = f"HTTPError {exc.code}; failed to read body: {read_exc}"
        else:
            result["bytes_read"] = len(error_body)
            text = error_body.decode("utf-8", errors="replace")
            result["error_body_preview"] = truncate_text(text, 400)
            result["error"] = f"HTTPError {exc.code}"
    except urllib.error.URLError as exc:
        result["error"] = f"URLError: {exc.reason}"
    except TimeoutError:
        result["error"] = "TimeoutError"
    except Exception as exc:  # pragma: no cover - defensive
        result["error"] = f"{type(exc).__name__}: {exc}"
    finally:
        result["elapsed_s"] = round(time.perf_counter() - start, 6)
        result["finished_at"] = iso_now()

    return result


def collect_metric(values: list[float | None]) -> dict[str, Any]:
    cleaned = [float(v) for v in values if v is not None]
    if not cleaned:
        return {"count": 0, "min": None, "max": None, "mean": None, "p50": None, "p90": None}
    return {
        "count": len(cleaned),
        "min": round(min(cleaned), 6),
        "max": round(max(cleaned), 6),
        "mean": round(sum(cleaned) / len(cleaned), 6),
        "p50": round(percentile(cleaned, 0.50), 6),
        "p90": round(percentile(cleaned, 0.90), 6),
    }


def percentile(values: list[float], ratio: float) -> float:
    if not values:
        raise ValueError("percentile requires non-empty values")
    ordered = sorted(values)
    if len(ordered) == 1:
        return ordered[0]
    pos = (len(ordered) - 1) * ratio
    lower = math.floor(pos)
    upper = math.ceil(pos)
    if lower == upper:
        return ordered[lower]
    weight = pos - lower
    return ordered[lower] * (1 - weight) + ordered[upper] * weight


def summarize(
    records: list[dict[str, Any]],
    instances: list[InstanceTarget],
    args: argparse.Namespace,
    body: bytes,
    jsonl_path: Path,
    summary_path: Path,
) -> dict[str, Any]:
    per_instance: list[dict[str, Any]] = []
    for instance in instances:
        subset = [r for r in records if r["instance_name"] == instance.name]
        statuses = Counter(str(r["status"]) if r["status"] is not None else "null" for r in subset)
        per_instance.append(
            {
                "instance_name": instance.name,
                "base_url": instance.base_url,
                "request_url": instance.request_url,
                "samples": len(subset),
                "status_counts": dict(sorted(statuses.items())),
                "success_count": sum(1 for r in subset if r["status"] == 200),
                "error_count": sum(1 for r in subset if r.get("error")),
                "first_line_s": collect_metric([r.get("first_line_s") for r in subset]),
                "first_data_s": collect_metric([r.get("first_data_s") for r in subset]),
                "first_delta_s": collect_metric([r.get("first_delta_s") for r in subset]),
                "elapsed_s": collect_metric([r.get("elapsed_s") for r in subset]),
            }
        )

    all_statuses = Counter(str(r["status"]) if r["status"] is not None else "null" for r in records)
    return {
        "script": str(Path(__file__).resolve()),
        "generated_at": iso_now(),
        "rounds_per_instance": args.rounds,
        "order": args.order,
        "timeout_s": args.timeout,
        "model": args.model,
        "prompt": args.prompt,
        "instances": [{"name": item.name, "base_url": item.base_url} for item in instances],
        "headers": {k: v for k, v in CLAUDE_CODE_HEADERS.items()},
        "request_body": json.loads(body.decode("utf-8")),
        "total_requests": len(records),
        "status_counts": dict(sorted(all_statuses.items())),
        "first_line_s": collect_metric([r.get("first_line_s") for r in records]),
        "first_data_s": collect_metric([r.get("first_data_s") for r in records]),
        "first_delta_s": collect_metric([r.get("first_delta_s") for r in records]),
        "elapsed_s": collect_metric([r.get("elapsed_s") for r in records]),
        "per_instance": per_instance,
        "artifacts": {
            "jsonl": str(jsonl_path),
            "summary_json": str(summary_path),
        },
    }


def print_progress(record: dict[str, Any], total: int) -> None:
    first_line = record["first_line_s"]
    first_data = record["first_data_s"]
    first_delta = record["first_delta_s"]
    status = record["status"]
    error = record["error"]
    print(
        (
            f"[{record['seq']:>3}/{total}] {record['instance_name']} "
            f"round={record['round']:>2} status={status} "
            f"first_line={first_line} first_data={first_data} "
            f"first_delta={first_delta} elapsed={record['elapsed_s']}"
            + (f" error={error}" if error else "")
        ),
        flush=True,
    )


def main() -> int:
    args = parse_args()
    api_key = ensure_api_key(args.api_key)
    if args.rounds <= 0:
        raise SystemExit("--rounds must be > 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be > 0")

    instances = parse_instances(args.instance)
    body = build_request_body(args.model, args.prompt)
    headers = build_headers(api_key)
    schedule = iter_schedule(instances, args.rounds, args.order)

    repo_root = Path(__file__).resolve().parents[1]
    results_dir = repo_root / "test-results"
    results_dir.mkdir(parents=True, exist_ok=True)

    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    stem = f"{args.output_prefix}_{timestamp}"
    jsonl_path = results_dir / f"{stem}.jsonl"
    summary_path = results_dir / f"{stem}.summary.json"

    records: list[dict[str, Any]] = []
    with jsonl_path.open("w", encoding="utf-8", newline="\n") as jsonl_file:
        total = len(schedule)
        for seq, (round_index, instance) in enumerate(schedule, start=1):
            record = make_request(
                instance=instance,
                body=body,
                headers=headers,
                timeout=args.timeout,
                seq=seq,
                round_index=round_index,
            )
            jsonl_file.write(json.dumps(record, ensure_ascii=False) + "\n")
            jsonl_file.flush()
            records.append(record)
            print_progress(record, total)

    summary = summarize(records, instances, args, body, jsonl_path, summary_path)
    summary_path.write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )

    print(f"\nJSONL  : {jsonl_path}")
    print(f"Summary: {summary_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
