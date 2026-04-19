import json
import os
import random
import time

from locust import HttpUser, task, between, events

TEST_USERS_FILE = os.getenv("TEST_USERS_FILE", "testing/test_users.json")

# Configurable wait times - lower = more aggressive load
WAIT_MIN = float(os.getenv("LOCUST_WAIT_MIN", "0.1"))
WAIT_MAX = float(os.getenv("LOCUST_WAIT_MAX", "0.5"))


def load_test_users():
    if not os.path.exists(TEST_USERS_FILE):
        raise FileNotFoundError(
            f"Test users file not found: {TEST_USERS_FILE}. "
            f"Run scripts/seed_test_data.sh first."
        )
    with open(TEST_USERS_FILE, "r", encoding="utf-8") as f:
        users = json.load(f)
    if not users:
        raise ValueError("No users found in test users file.")
    return users


TEST_USERS = load_test_users()


class StressUser(HttpUser):
    """
    High-throughput Locust user for resilience stress testing.

    Task weights match the proposal's load profile:
      - 70% reads (timeline)
      - 20% likes
      - 10% writes (tweets)
    Mapped to task weights: 5 home_timeline + 2 user_timeline = 7 reads,
    2 likes, 1 tweet write -> 70:20:10 ratio.
    """

    wait_time = between(WAIT_MIN, WAIT_MAX)

    def on_start(self):
        self.user = random.choice(TEST_USERS)
        self.token = self.user["token"]
        self.user_id = self.user["user_id"]
        self.tweet_ids = []

    def _headers(self):
        return {"Authorization": f"Bearer {self.token}"}

    @task(5)
    def get_home_timeline(self):
        """Primary read path -- exercises Redis cache + DB replica fallback."""
        with self.client.get(
            "/v1/timeline/home",
            headers=self._headers(),
            name="GET /v1/timeline/home",
            catch_response=True,
        ) as resp:
            if resp.status_code == 200:
                try:
                    tweets = resp.json()
                    if tweets and isinstance(tweets, list):
                        for t in tweets[:5]:
                            tid = t.get("id")
                            if tid and tid not in self.tweet_ids:
                                self.tweet_ids.append(tid)
                        self.tweet_ids = self.tweet_ids[-50:]
                except Exception:
                    pass
                resp.success()
            elif resp.status_code == 429:
                resp.failure("Rate limited")
            else:
                resp.failure(f"Status {resp.status_code}")

    @task(2)
    def get_user_timeline(self):
        """Secondary read path -- exercises DB replicas directly."""
        target_user = random.choice(TEST_USERS)
        with self.client.get(
            f"/v1/timeline/user/{target_user['user_id']}",
            headers=self._headers(),
            name="GET /v1/timeline/user/:id",
            catch_response=True,
        ) as resp:
            if resp.status_code == 200:
                resp.success()
            elif resp.status_code == 429:
                resp.failure("Rate limited")
            else:
                resp.failure(f"Status {resp.status_code}")

    @task(1)
    def post_tweet(self):
        """Write path -- exercises primary DB."""
        payload = {
            "content": f"stress test {self.user['username']} {int(time.time()*1000)}"
        }
        with self.client.post(
            "/v1/tweets",
            headers={**self._headers(), "Content-Type": "application/json"},
            json=payload,
            name="POST /v1/tweets",
            catch_response=True,
        ) as resp:
            if resp.status_code in (200, 201):
                try:
                    tid = resp.json().get("id")
                    if tid:
                        self.tweet_ids.append(tid)
                        self.tweet_ids = self.tweet_ids[-50:]
                except Exception:
                    pass
                resp.success()
            elif resp.status_code == 429:
                resp.failure("Rate limited")
            else:
                resp.failure(f"Status {resp.status_code}")

    @task(2)
    def like_tweet(self):
        """Like path -- exercises consistency mode."""
        if not self.tweet_ids:
            return
        tweet_id = random.choice(self.tweet_ids)
        with self.client.post(
            f"/v1/tweets/{tweet_id}/like",
            headers=self._headers(),
            name="POST /v1/tweets/:id/like",
            catch_response=True,
        ) as resp:
            if resp.status_code in (200, 201, 204, 409):  # 204 = success, 409 = already liked
                resp.success()
            elif resp.status_code == 429:
                resp.failure("Rate limited")
            else:
                resp.failure(f"Status {resp.status_code}")
