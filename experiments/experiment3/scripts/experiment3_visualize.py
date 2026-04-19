#!/usr/bin/env python3
"""
Experiment 3 Visualization: Generate annotated charts from Locust CSV data.

Produces charts similar to the Locust UI but with failure injection points
marked and before/during/after phases highlighted.

Generates:
  - Per-scenario: response time over time, throughput over time, error rate
  - Combined comparison chart across all scenarios

Usage (run from project root):
    python3 experiments/experiment3/scripts/experiment3_visualize.py experiments/experiment3/results/<run_id>
"""

import csv
import os
import sys
from datetime import datetime, timezone, timedelta

import matplotlib
matplotlib.use("Agg")  # non-interactive backend
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
from matplotlib.patches import Rectangle


def safe_float(v, default=0.0):
    try:
        return float(v)
    except (ValueError, TypeError):
        return default


def load_history(results_dir, scenario):
    """Load per-second Aggregated rows from locust_stats_history.csv."""
    path = os.path.join(results_dir, scenario, "locust_stats_history.csv")
    rows = []
    with open(path) as f:
        for row in csv.DictReader(f):
            if row.get("Name") == "Aggregated":
                ts = datetime.fromtimestamp(int(row["Timestamp"]), tz=timezone.utc)
                rows.append({
                    "ts": ts,
                    "epoch": int(row["Timestamp"]),
                    "avg": safe_float(row.get("Total Average Response Time")),
                    "p50": safe_float(row.get("Total Median Response Time")),
                    "p95": safe_float(row.get("95%")),
                    "p99": safe_float(row.get("99%")),
                    "rps": safe_float(row.get("Requests/s")),
                    "fail_s": safe_float(row.get("Failures/s")),
                })
    return rows


def load_events(results_dir):
    """Load experiment_log.csv events."""
    events = []
    path = os.path.join(results_dir, "experiment_log.csv")
    with open(path) as f:
        for line in f:
            parts = line.strip().split(",", 3)
            if len(parts) >= 4 and parts[0].startswith("2026"):
                try:
                    ts = datetime.fromisoformat(parts[0].replace("Z", "+00:00"))
                    events.append({"ts": ts, "scenario": parts[1], "phase": parts[2], "msg": parts[3]})
                except ValueError:
                    continue
    return events


def find_event_ts(events, scenario, phase_kw, msg_kw):
    """Find timestamp of a specific event."""
    for e in events:
        if e["scenario"] == scenario and phase_kw in e["phase"] and msg_kw in e["msg"]:
            return e["ts"]
    return None


def smooth(values, window=5):
    """Simple moving average."""
    result = []
    for i in range(len(values)):
        start = max(0, i - window // 2)
        end = min(len(values), i + window // 2 + 1)
        result.append(sum(values[start:end]) / (end - start))
    return result


def plot_scenario(rows, title, inject_ts, inject_label, output_path, recovery_ts=None):
    """Generate a 3-panel chart for a single scenario."""
    if not rows:
        print(f"  No data for {title}, skipping")
        return

    # Convert to relative time in seconds from start
    start_epoch = rows[0]["epoch"]
    times_sec = [(r["epoch"] - start_epoch) for r in rows]
    times_min = [t / 60.0 for t in times_sec]

    avg = [r["avg"] for r in rows]
    p95 = [r["p95"] for r in rows]
    p99 = [r["p99"] for r in rows]
    rps = [r["rps"] for r in rows]
    fail = [r["fail_s"] for r in rows]

    # Smooth the data for readability (5-second window)
    avg_s = smooth(avg, 10)
    p95_s = smooth(p95, 10)
    p99_s = smooth(p99, 10)
    rps_s = smooth(rps, 10)
    fail_s = smooth(fail, 10)

    # Find inject time relative to start
    inject_min = None
    if inject_ts:
        inject_sec = (inject_ts - rows[0]["ts"]).total_seconds()
        inject_min = inject_sec / 60.0

    recovery_min = None
    if recovery_ts:
        recovery_sec = (recovery_ts - rows[0]["ts"]).total_seconds()
        recovery_min = recovery_sec / 60.0

    fig, axes = plt.subplots(3, 1, figsize=(14, 10), sharex=True)
    fig.suptitle(title, fontsize=14, fontweight="bold")

    # Panel 1: Response Time
    ax1 = axes[0]
    ax1.fill_between(times_min, p99_s, alpha=0.15, color="red", label="p99")
    ax1.fill_between(times_min, p95_s, alpha=0.2, color="orange", label="p95")
    ax1.plot(times_min, avg_s, color="green", linewidth=1.5, label="Avg")
    ax1.set_ylabel("Response Time (ms)")
    ax1.legend(loc="upper left", fontsize=9)
    ax1.grid(True, alpha=0.3)
    ax1.set_ylim(bottom=0)

    # Panel 2: Throughput
    ax2 = axes[1]
    ax2.plot(times_min, rps_s, color="blue", linewidth=1.5, label="Requests/s")
    ax2.set_ylabel("Throughput (req/s)")
    ax2.legend(loc="upper left", fontsize=9)
    ax2.grid(True, alpha=0.3)
    ax2.set_ylim(bottom=0)

    # Panel 3: Error Rate
    ax3 = axes[2]
    ax3.plot(times_min, fail_s, color="red", linewidth=1.5, label="Failures/s")
    ax3.set_ylabel("Failures/s")
    ax3.set_xlabel("Time (minutes)")
    ax3.legend(loc="upper left", fontsize=9)
    ax3.grid(True, alpha=0.3)
    ax3.set_ylim(bottom=0)

    # Add failure injection marker
    if inject_min is not None and 0 < inject_min < times_min[-1]:
        for ax in axes:
            ax.axvline(x=inject_min, color="red", linestyle="--", linewidth=2, alpha=0.8)
        # Add label at top
        axes[0].annotate(
            inject_label,
            xy=(inject_min, axes[0].get_ylim()[1] * 0.95),
            fontsize=10, color="red", fontweight="bold",
            ha="center", va="top",
            bbox=dict(boxstyle="round,pad=0.3", facecolor="white", edgecolor="red", alpha=0.9),
        )

    # Add recovery marker
    if recovery_min is not None and 0 < recovery_min < times_min[-1]:
        for ax in axes:
            ax.axvline(x=recovery_min, color="green", linestyle="--", linewidth=2, alpha=0.8)
        axes[0].annotate(
            "Recovery",
            xy=(recovery_min, axes[0].get_ylim()[1] * 0.85),
            fontsize=10, color="green", fontweight="bold",
            ha="center", va="top",
            bbox=dict(boxstyle="round,pad=0.3", facecolor="white", edgecolor="green", alpha=0.9),
        )

    plt.tight_layout()
    plt.savefig(output_path, dpi=150, bbox_inches="tight")
    plt.close()
    print(f"  Saved: {output_path}")


def plot_comparison(results_dir, scenarios_data):
    """Generate a side-by-side comparison chart."""
    fig, axes = plt.subplots(2, 1, figsize=(14, 8))
    fig.suptitle("Experiment 3: Scenario Comparison", fontsize=14, fontweight="bold")

    colors = {"baseline": "green", "replica_failure": "orange", "redis_flush": "red"}
    labels = {"baseline": "Baseline (healthy)", "replica_failure": "Replica Failure", "redis_flush": "Redis Flush"}

    # Panel 1: Avg response time comparison
    ax1 = axes[0]
    for scenario, rows in scenarios_data.items():
        if not rows:
            continue
        start_epoch = rows[0]["epoch"]
        times_min = [(r["epoch"] - start_epoch) / 60.0 for r in rows]
        avg_s = smooth([r["avg"] for r in rows], 10)
        ax1.plot(times_min, avg_s, color=colors.get(scenario, "gray"),
                 linewidth=1.5, label=labels.get(scenario, scenario))

    ax1.set_ylabel("Avg Response Time (ms)")
    ax1.legend(loc="upper left")
    ax1.grid(True, alpha=0.3)
    ax1.set_ylim(bottom=0)

    # Panel 2: Throughput comparison
    ax2 = axes[1]
    for scenario, rows in scenarios_data.items():
        if not rows:
            continue
        start_epoch = rows[0]["epoch"]
        times_min = [(r["epoch"] - start_epoch) / 60.0 for r in rows]
        rps_s = smooth([r["rps"] for r in rows], 10)
        ax2.plot(times_min, rps_s, color=colors.get(scenario, "gray"),
                 linewidth=1.5, label=labels.get(scenario, scenario))

    ax2.set_ylabel("Throughput (req/s)")
    ax2.set_xlabel("Time (minutes)")
    ax2.legend(loc="upper left")
    ax2.grid(True, alpha=0.3)
    ax2.set_ylim(bottom=0)

    output_path = os.path.join(results_dir, "comparison_chart.png")
    plt.tight_layout()
    plt.savefig(output_path, dpi=150, bbox_inches="tight")
    plt.close()
    print(f"  Saved: {output_path}")


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <results_directory>")
        sys.exit(1)

    results_dir = sys.argv[1]
    if not os.path.isdir(results_dir):
        print(f"ERROR: {results_dir} is not a directory")
        sys.exit(1)

    print(f"\nGenerating charts from: {results_dir}\n")

    events = load_events(results_dir)
    scenarios_data = {}

    # Scenario 1: Baseline
    print("Scenario 1: Baseline")
    rows = load_history(results_dir, "baseline")
    scenarios_data["baseline"] = rows
    plot_scenario(
        rows,
        "Scenario 1: Baseline (All Components Healthy)",
        inject_ts=None, inject_label="",
        output_path=os.path.join(results_dir, "baseline", "chart_baseline.png"),
    )

    # Scenario 2: Replica Failure
    print("Scenario 2: Replica Failure")
    rows = load_history(results_dir, "replica_failure")
    scenarios_data["replica_failure"] = rows
    inject = find_event_ts(events, "replica_failure", "INJECT", "Rebooting")
    recovery = find_event_ts(events, "replica_failure", "RECOVERY", "available")
    plot_scenario(
        rows,
        "Scenario 2: Replica Failure (Read Replica Rebooted)",
        inject_ts=inject, inject_label="REPLICA REBOOT",
        output_path=os.path.join(results_dir, "replica_failure", "chart_replica_failure.png"),
        recovery_ts=recovery,
    )

    # Scenario 3: Redis Flush
    print("Scenario 3: Redis Flush")
    rows = load_history(results_dir, "redis_flush")
    scenarios_data["redis_flush"] = rows
    flush = find_event_ts(events, "redis_flush", "INJECT", "Flushing")
    plot_scenario(
        rows,
        "Scenario 3: Redis Cache Flush (FLUSHALL)",
        inject_ts=flush, inject_label="REDIS FLUSHALL",
        output_path=os.path.join(results_dir, "redis_flush", "chart_redis_flush.png"),
    )

    # Comparison chart
    print("Comparison")
    plot_comparison(results_dir, scenarios_data)

    print(f"\nDone! Charts saved to {results_dir}/")
    print(f"\nFiles generated:")
    print(f"  baseline/chart_baseline.png          -- Steady-state performance")
    print(f"  baseline/locust_report.html           -- Locust HTML report")
    print(f"  replica_failure/chart_replica_failure.png -- Latency/throughput with failure marker")
    print(f"  replica_failure/locust_report.html    -- Locust HTML report")
    print(f"  redis_flush/chart_redis_flush.png     -- Latency/throughput with flush marker")
    print(f"  redis_flush/locust_report.html        -- Locust HTML report")
    print(f"  comparison_chart.png                  -- All scenarios overlaid")


if __name__ == "__main__":
    main()
