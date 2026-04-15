"""Refresh expired JWT tokens in testing/test_users.json by re-logging in."""
import json
import sys
import requests
from concurrent.futures import ThreadPoolExecutor, as_completed

BASE_URL = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
USERS_FILE = "testing/test_users.json"

with open(USERS_FILE) as f:
    users = json.load(f)

print(f"Refreshing tokens for {len(users)} users against {BASE_URL} ...")

def refresh(user):
    try:
        resp = requests.post(f"{BASE_URL}/v1/auth/login", json={
            "username": user["username"],
            "password": user["password"],
        }, timeout=10)
        if resp.status_code == 200:
            user["token"] = resp.json()["token"]
            return True
        else:
            print(f"  FAILED {user['username']}: {resp.status_code} {resp.text[:80]}")
            return False
    except Exception as e:
        print(f"  ERROR {user['username']}: {e}")
        return False

ok, failed = 0, 0
with ThreadPoolExecutor(max_workers=20) as pool:
    futures = {pool.submit(refresh, u): u for u in users}
    for i, future in enumerate(as_completed(futures), 1):
        if future.result():
            ok += 1
        else:
            failed += 1
        if i % 50 == 0 or i == len(users):
            print(f"  Progress: {i}/{len(users)} done ({ok} ok, {failed} failed)")

with open(USERS_FILE, "w") as f:
    json.dump(users, f, indent=2)

print(f"\nDone: {ok} refreshed, {failed} failed.")
