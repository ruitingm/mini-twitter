"""
Staleness Probe — Experiment 2: Strong vs. Eventual Consistency

This script demonstrates the "inconsistency window" in eventual consistency mode.
It likes a target tweet, then polls GET /v1/tweets/{id} every few seconds to watch
whether the like_count reflects the writes immediately (strong) or only after the
aggregator flush interval (eventual).

Usage:
    # 1. Pick any tweet ID from your seeded data (or run once to get one):
    python3 scripts/staleness_probe.py --host http://localhost:8080 \\
        --tweet-id <tweet-uuid> --token <jwt-token> --duration 90

    # 2. To find a tweet ID quickly:
    curl -s http://localhost:8080/v1/timeline/home \\
        -H "Authorization: Bearer <token>" | python3 -m json.tool | grep '"id"' | head -3

Output:
    T+00s  like_count=42  (sent 1 like)
    T+05s  like_count=42  stale  (delta not yet flushed)
    T+10s  like_count=42  stale
    T+30s  like_count=47  FLUSHED  <-- aggregator ran, gap closed

Screenshot this terminal output to show the staleness window.
"""

import argparse
import time
import requests


def parse_args():
    p = argparse.ArgumentParser(description="Staleness probe for like count consistency")
    p.add_argument("--host", default="http://localhost:8080", help="API base URL")
    p.add_argument("--tweet-id", required=True, help="UUID of the tweet to monitor")
    p.add_argument("--token", required=True, help="JWT bearer token of the liking user")
    p.add_argument("--interval", type=float, default=5.0, help="Poll interval in seconds (default: 5)")
    p.add_argument("--duration", type=float, default=90.0, help="Total probe duration in seconds (default: 90)")
    p.add_argument("--likes-per-round", type=int, default=5, help="Likes to send before each poll (default: 5)")
    return p.parse_args()


def get_like_count(host: str, tweet_id: str) -> int | None:
    try:
        resp = requests.get(f"{host}/v1/tweets/{tweet_id}", timeout=5)
        if resp.status_code == 200:
            return resp.json().get("like_count")
    except requests.RequestException as e:
        print(f"  [read error] {e}")
    return None


def send_likes(host: str, tweet_id: str, token: str, count: int) -> int:
    """Send `count` likes and return how many succeeded (204)."""
    headers = {"Authorization": f"Bearer {token}"}
    ok = 0
    for _ in range(count):
        try:
            r = requests.post(f"{host}/v1/tweets/{tweet_id}/like", headers=headers, timeout=5)
            if r.status_code == 204:
                ok += 1
            else:
                # Already liked — unlike to reset, then like again
                requests.delete(f"{host}/v1/tweets/{tweet_id}/like", headers=headers, timeout=5)
                r2 = requests.post(f"{host}/v1/tweets/{tweet_id}/like", headers=headers, timeout=5)
                if r2.status_code == 204:
                    ok += 1
        except requests.RequestException:
            pass
    return ok


def main():
    args = parse_args()
    host = args.host.rstrip("/")

    print(f"\n{'='*60}")
    print(f"  Staleness Probe")
    print(f"  Tweet : {args.tweet_id}")
    print(f"  Host  : {host}")
    print(f"  Poll  : every {args.interval}s for {args.duration}s")
    print(f"{'='*60}\n")

    baseline = get_like_count(host, args.tweet_id)
    if baseline is None:
        print("ERROR: could not read tweet. Check --tweet-id and --host.")
        return

    print(f"  Baseline like_count = {baseline}\n")
    print(f"  {'Time':>6}  {'like_count':>12}  {'delta_from_baseline':>20}  Note")
    print(f"  {'-'*6}  {'-'*12}  {'-'*20}  {'-'*20}")

    start = time.time()
    total_likes_sent = 0
    prev_count = baseline

    while True:
        elapsed = time.time() - start
        if elapsed > args.duration:
            break

        # Send a burst of likes before reading
        sent = send_likes(host, args.tweet_id, args.token, args.likes_per_round)
        total_likes_sent += sent

        count = get_like_count(host, args.tweet_id)
        if count is None:
            count = prev_count

        delta = count - baseline
        jumped = count > prev_count

        note = ""
        if jumped:
            note = "<-- FLUSHED (aggregator ran)"
        elif delta < total_likes_sent:
            note = f"stale  (expected >={baseline + total_likes_sent}, got {count})"

        print(f"  T+{int(elapsed):02d}s  like_count={count:<8}  delta={delta:<20}  {note}")

        prev_count = count
        time.sleep(args.interval)

    final = get_like_count(host, args.tweet_id)
    print(f"\n  {'='*60}")
    print(f"  Done. Sent {total_likes_sent} likes over {args.duration}s.")
    print(f"  Final like_count = {final}  (baseline was {baseline})")
    print(f"  {'='*60}\n")


if __name__ == "__main__":
    main()
