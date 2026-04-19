#!/usr/bin/env python3
"""
Experiment 3 Analysis: Compare baseline vs failure scenarios.

Reads Locust CSV outputs from each scenario, splits time-series data into
pre-failure / during-failure / post-recovery windows using the experiment log,
and generates a comparison report.

Usage (run from project root):
    python3 experiments/experiment3/scripts/experiment3_analyze.py experiments/experiment3/results/<run_id>
"""

import csv
import json
import sys
import os
from datetime import datetime, timezone


def parse_ts(s):
    """Parse ISO timestamp to datetime."""
    s = s.replace("Z", "+00:00")
    return datetime.fromisoformat(s)


def safe_float(v, default=0.0):
    try:
        return float(v)
    except (ValueError, TypeError):
        return default


def load_experiment_log(results_dir):
    """Load experiment_log.csv, parsing manually to handle commas in messages."""
    log_path = os.path.join(results_dir, "experiment_log.csv")
    events = []
    with open(log_path) as f:
        next(f)  # skip header
        for line in f:
            line = line.strip()
            if not line or not line[0].isdigit():
                continue
            # Split into exactly 4 fields: timestamp, scenario, phase, event (rest)
            parts = line.split(",", 3)
            if len(parts) >= 4:
                events.append({
                    "timestamp": parts[0],
                    "scenario": parts[1],
                    "phase": parts[2],
                    "event": parts[3],
                })
    return events


def get_phase_boundaries(events, scenario):
    """Extract key timestamps for a scenario from the event log."""
    boundaries = {}
    for e in events:
        if e["scenario"] != scenario:
            continue
        phase = e["phase"]
        ts = parse_ts(e["timestamp"])
        key = f"{phase}"
        if key not in boundaries:
            boundaries[key] = ts
        else:
            boundaries[f"{key}_end"] = ts
    return boundaries


def load_locust_history(results_dir, scenario):
    """Load locust_stats_history.csv for a scenario, return Aggregated rows."""
    path = os.path.join(results_dir, scenario, "locust_stats_history.csv")
    if not os.path.exists(path):
        return []
    rows = []
    with open(path) as f:
        reader = csv.DictReader(f)
        for row in reader:
            if row.get("Name") == "Aggregated":
                row["_ts"] = datetime.utcfromtimestamp(
                    int(row["Timestamp"])
                ).replace(tzinfo=timezone.utc)
                rows.append(row)
    return rows


def load_locust_stats(results_dir, scenario):
    """Load the final locust_stats.csv summary."""
    path = os.path.join(results_dir, scenario, "locust_stats.csv")
    if not os.path.exists(path):
        return []
    rows = []
    with open(path) as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append(row)
    return rows


def summarize_window(rows, start, end):
    """Compute summary stats for rows within a time window."""
    window = [r for r in rows if start <= r["_ts"] < end]
    if not window:
        return None

    n = len(window)
    avg_resp = sum(safe_float(r.get("Total Average Response Time", 0)) for r in window) / n
    avg_p50 = sum(safe_float(r.get("Total Median Response Time", 0)) for r in window) / n
    avg_p95 = sum(safe_float(r.get("95%", 0)) for r in window) / n
    avg_p99 = sum(safe_float(r.get("99%", 0)) for r in window) / n
    avg_rps = sum(safe_float(r.get("Requests/s", 0)) for r in window) / n
    avg_fail = sum(safe_float(r.get("Failures/s", 0)) for r in window) / n

    # Calculate total failures in window
    total_reqs = sum(safe_float(r.get("Total Request Count", 0)) for r in window)
    total_fails = sum(safe_float(r.get("Total Failure Count", 0)) for r in window)

    return {
        "samples": n,
        "avg_ms": avg_resp,
        "p50": avg_p50,
        "p95": avg_p95,
        "p99": avg_p99,
        "rps": avg_rps,
        "fail_s": avg_fail,
        "duration_s": (end - start).total_seconds(),
    }


def print_summary_table(label, stats):
    """Print a formatted summary row."""
    if stats is None:
        print(f"  {label:<25} {'N/A':>8}")
        return
    print(
        f"  {label:<25} {stats['avg_ms']:>8.1f} {stats['p50']:>7.0f} "
        f"{stats['p95']:>7.0f} {stats['p99']:>7.0f} {stats['rps']:>8.1f} "
        f"{stats['fail_s']:>7.3f}"
    )


def analyze_baseline(results_dir, events):
    """Analyze baseline scenario."""
    print("=" * 80)
    print("SCENARIO 1: BASELINE (all components healthy)")
    print("=" * 80)

    stats = load_locust_stats(results_dir, "baseline")
    history = load_locust_history(results_dir, "baseline")
    bounds = get_phase_boundaries(events, "baseline")

    if not stats:
        print("  No data found for baseline scenario.")
        return None

    # Print overall stats
    for row in stats:
        if row.get("Name") == "Aggregated":
            print(f"\n  Total Requests:  {row.get('Request Count', 'N/A')}")
            print(f"  Total Failures:  {row.get('Failure Count', 'N/A')}")
            print(f"  Avg Response:    {safe_float(row.get('Average Response Time', 0)):.1f} ms")
            print(f"  p50:             {safe_float(row.get('50%', 0)):.0f} ms")
            print(f"  p95:             {safe_float(row.get('95%', 0)):.0f} ms")
            print(f"  p99:             {safe_float(row.get('99%', 0)):.0f} ms")
            print(f"  Throughput:      {safe_float(row.get('Requests/s', 0)):.1f} req/s")
            break

    # Per-endpoint breakdown
    print(f"\n  {'Endpoint':<35} {'Reqs':>7} {'Fails':>6} {'Avg':>7} {'p95':>7} {'p99':>7}")
    print("  " + "-" * 73)
    for row in stats:
        name = row.get("Name", "")
        if name and name != "Aggregated":
            print(
                f"  {name:<35} {row.get('Request Count',''):>7} "
                f"{row.get('Failure Count',''):>6} "
                f"{safe_float(row.get('Average Response Time',0)):>7.1f} "
                f"{safe_float(row.get('95%',0)):>7.0f} "
                f"{safe_float(row.get('99%',0)):>7.0f}"
            )

    return stats


def analyze_replica_failure(results_dir, events, baseline_stats):
    """Analyze replica failure scenario."""
    print("\n")
    print("=" * 80)
    print("SCENARIO 2: REPLICA FAILURE")
    print("=" * 80)

    history = load_locust_history(results_dir, "replica_failure")
    stats = load_locust_stats(results_dir, "replica_failure")
    bounds = get_phase_boundaries(events, "replica_failure")

    if not history:
        print("  No data found for replica failure scenario.")
        return

    # Determine time windows from event log
    inject_ts = None
    recovery_ts = None
    for e in events:
        if e["scenario"] == "replica_failure":
            if e["phase"] == "INJECT" and "Rebooting" in e.get("event", ""):
                inject_ts = parse_ts(e["timestamp"])
            if e["phase"] == "RECOVERY" and "available" in e.get("event", ""):
                recovery_ts = parse_ts(e["timestamp"])

    if not inject_ts:
        print("  Could not find failure injection timestamp.")
        return

    # Define windows
    earliest = history[0]["_ts"]
    latest = history[-1]["_ts"]

    pre_start = inject_ts.__class__(
        inject_ts.year, inject_ts.month, inject_ts.day,
        inject_ts.hour, inject_ts.minute, inject_ts.second,
        tzinfo=inject_ts.tzinfo
    )
    # Go back 2 minutes for pre-failure
    from datetime import timedelta
    pre_window_start = inject_ts - timedelta(seconds=120)
    during_window_end = recovery_ts if recovery_ts else inject_ts + timedelta(seconds=180)

    pre = summarize_window(history, pre_window_start, inject_ts)
    during = summarize_window(history, inject_ts, during_window_end)
    post = summarize_window(history, during_window_end, latest) if recovery_ts else None

    print(f"\n  Failure injected at: {inject_ts.strftime('%H:%M:%S')} UTC")
    if recovery_ts:
        recovery_secs = (recovery_ts - inject_ts).total_seconds()
        print(f"  Recovery detected:   {recovery_ts.strftime('%H:%M:%S')} UTC ({recovery_secs:.0f}s)")

    print(f"\n  {'Window':<25} {'Avg(ms)':>8} {'p50':>7} {'p95':>7} {'p99':>7} {'Req/s':>8} {'Fail/s':>7}")
    print("  " + "-" * 73)
    print_summary_table("Pre-failure", pre)
    print_summary_table("During failure", during)
    print_summary_table("Post-recovery", post)

    # Compare with baseline
    if baseline_stats and pre and during:
        print(f"\n  Impact Analysis:")
        if pre["avg_ms"] > 0:
            latency_change = ((during["avg_ms"] - pre["avg_ms"]) / pre["avg_ms"]) * 100
            print(f"    Latency change:     {latency_change:+.1f}% (pre: {pre['avg_ms']:.1f}ms -> during: {during['avg_ms']:.1f}ms)")
        if pre["rps"] > 0:
            throughput_change = ((during["rps"] - pre["rps"]) / pre["rps"]) * 100
            print(f"    Throughput change:   {throughput_change:+.1f}% (pre: {pre['rps']:.1f} -> during: {during['rps']:.1f} req/s)")
        print(f"    Failure rate:        {during['fail_s']:.3f}/s during failure")
        if pre["p95"] > 0:
            p95_change = ((during["p95"] - pre["p95"]) / pre["p95"]) * 100
            print(f"    p95 change:          {p95_change:+.1f}% (pre: {pre['p95']:.0f}ms -> during: {during['p95']:.0f}ms)")

        # Check against proposal targets
        print(f"\n  Proposal Targets:")
        if pre["avg_ms"] > 0:
            lat_pct = abs((during["avg_ms"] - pre["avg_ms"]) / pre["avg_ms"]) * 100
            target_met = lat_pct < 25
            print(f"    Latency increase <25%:   {'PASS' if target_met else 'FAIL'} ({lat_pct:.1f}%)")
        if during.get("duration_s", 0) > 0:
            # Rough error rate
            total_reqs_during = during["rps"] * during["duration_s"]
            total_fails_during = during["fail_s"] * during["duration_s"]
            err_rate = (total_fails_during / total_reqs_during * 100) if total_reqs_during > 0 else 0
            target_met = err_rate < 1
            print(f"    Error rate <1%:          {'PASS' if target_met else 'FAIL'} ({err_rate:.2f}%)")


def analyze_redis_flush(results_dir, events, baseline_stats):
    """Analyze Redis flush scenario."""
    print("\n")
    print("=" * 80)
    print("SCENARIO 3: REDIS CACHE FLUSH")
    print("=" * 80)

    history = load_locust_history(results_dir, "redis_flush")
    stats = load_locust_stats(results_dir, "redis_flush")

    if not history:
        print("  No data found for Redis flush scenario.")
        return

    # Find flush timestamp
    flush_ts = None
    for e in events:
        if e["scenario"] == "redis_flush" and e["phase"] == "INJECT":
            flush_ts = parse_ts(e["timestamp"])

    if not flush_ts:
        print("  Could not find flush timestamp.")
        return

    from datetime import timedelta

    earliest = history[0]["_ts"]
    latest = history[-1]["_ts"]

    pre = summarize_window(history, flush_ts - timedelta(seconds=120), flush_ts)
    # First 60s after flush (immediate impact)
    immediate = summarize_window(history, flush_ts, flush_ts + timedelta(seconds=60))
    # Sustained (60-180s after flush)
    sustained = summarize_window(history, flush_ts + timedelta(seconds=60), latest)

    print(f"\n  Cache flushed at: {flush_ts.strftime('%H:%M:%S')} UTC")

    print(f"\n  {'Window':<25} {'Avg(ms)':>8} {'p50':>7} {'p95':>7} {'p99':>7} {'Req/s':>8} {'Fail/s':>7}")
    print("  " + "-" * 73)
    print_summary_table("Pre-flush (warm cache)", pre)
    print_summary_table("0-60s post-flush", immediate)
    print_summary_table("60-180s post-flush", sustained)

    if pre and immediate:
        print(f"\n  Impact Analysis:")
        if pre["avg_ms"] > 0:
            latency_change = ((immediate["avg_ms"] - pre["avg_ms"]) / pre["avg_ms"]) * 100
            latency_ratio = immediate["avg_ms"] / pre["avg_ms"]
            print(f"    Immediate latency:   {latency_change:+.1f}% ({latency_ratio:.1f}x) "
                  f"(pre: {pre['avg_ms']:.1f}ms -> post: {immediate['avg_ms']:.1f}ms)")
        if pre["rps"] > 0:
            throughput_change = ((immediate["rps"] - pre["rps"]) / pre["rps"]) * 100
            print(f"    Throughput change:    {throughput_change:+.1f}%")
        print(f"    Failure rate:         {immediate['fail_s']:.3f}/s (immediate)")

        if sustained:
            if pre["avg_ms"] > 0:
                recovery_ratio = sustained["avg_ms"] / pre["avg_ms"]
                print(f"    Sustained latency:   {recovery_ratio:.1f}x baseline "
                      f"({sustained['avg_ms']:.1f}ms)")

        print(f"\n  Proposal Expectations:")
        if pre["avg_ms"] > 0:
            ratio = immediate["avg_ms"] / pre["avg_ms"]
            in_range = 1.0 <= ratio <= 6.0
            print(f"    Expected 3-5x latency increase: {'~MATCH' if 2.0 <= ratio <= 6.0 else 'BELOW EXPECTED' if ratio < 2.0 else 'ABOVE EXPECTED'} ({ratio:.1f}x)")
        total_fail_immediate = immediate["fail_s"] * 60  # over 60s
        total_req_immediate = immediate["rps"] * 60
        functional = (total_fail_immediate / total_req_immediate * 100 < 5) if total_req_immediate > 0 else True
        print(f"    System remains functional: {'PASS' if functional else 'FAIL'}")


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <results_directory>")
        sys.exit(1)

    results_dir = sys.argv[1]

    if not os.path.isdir(results_dir):
        print(f"ERROR: {results_dir} is not a directory")
        sys.exit(1)

    events = load_experiment_log(results_dir)

    print("\n" + "=" * 80)
    print("  EXPERIMENT 3: FAILURE RESILIENCE TEST ANALYSIS")
    print("=" * 80)
    print(f"  Results dir: {results_dir}")
    print(f"  Events logged: {len(events)}")
    print()

    baseline_stats = analyze_baseline(results_dir, events)
    analyze_replica_failure(results_dir, events, baseline_stats)
    analyze_redis_flush(results_dir, events, baseline_stats)

    print("\n" + "=" * 80)
    print("  END OF ANALYSIS")
    print("=" * 80 + "\n")


if __name__ == "__main__":
    main()
