import json
import os
import random
import time

from locust import HttpUser, task, between, events

TEST_USERS_FILE = os.getenv("TEST_USERS_FILE", "testing/test_users.json")


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


class ResilienceUser(HttpUser):
    """
    Locust user for Experiment 3: Failure Resilience Tests.

    Workload design (matches proposal):
    - 3x timeline reads (home) -- exercises Redis cache + DB fallback
    - 2x timeline reads (user) -- exercises DB read replicas
    - 1x tweet creation        -- exercises write path
    - 1x like action           -- exercises consistency path
    """

    wait_time = between(1, 2)

    def on_start(self):
        self.user = random.choice(TEST_USERS)
        self.token = self.user["token"]
        self.user_id = self.user["user_id"]
        self.tweet_ids = []  # collect tweet IDs for liking

    def _auth_headers(self):
        return {"Authorization": f"Bearer {self.token}"}

    @task(3)
    def get_home_timeline(self):
        """Primary read workload -- exercises Redis cache and DB fallback."""
        with self.client.get(
            "/v1/timeline/home",
            headers=self._auth_headers(),
            name="GET /v1/timeline/home",
            catch_response=True,
        ) as resp:
            if resp.status_code == 200:
                try:
                    tweets = resp.json()
                    if tweets and isinstance(tweets, list):
                        # Collect tweet IDs for the like task
                        for t in tweets[:5]:
                            tid = t.get("id")
                            if tid and tid not in self.tweet_ids:
                                self.tweet_ids.append(tid)
                        # Keep the list bounded
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
        """Secondary read workload -- exercises DB read replicas directly."""
        target_user = random.choice(TEST_USERS)
        self.client.get(
            f"/v1/timeline/user/{target_user['user_id']}",
            headers=self._auth_headers(),
            name="GET /v1/timeline/user/:id",
        )

    @task(1)
    def post_tweet(self):
        """Write workload -- exercises primary DB."""
        payload = {
            "content": f"resilience test from {self.user['username']} at {int(time.time() * 1000)}"
        }
        with self.client.post(
            "/v1/tweets",
            headers={
                **self._auth_headers(),
                "Content-Type": "application/json",
            },
            json=payload,
            name="POST /v1/tweets",
            catch_response=True,
        ) as resp:
            if resp.status_code in (200, 201):
                try:
                    data = resp.json()
                    tid = data.get("id")
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

    @task(1)
    def like_tweet(self):
        """Like workload -- exercises consistency mode (eventual vs strong)."""
        if not self.tweet_ids:
            return  # skip until we have tweet IDs from timeline reads

        tweet_id = random.choice(self.tweet_ids)
        self.client.post(
            f"/v1/tweets/{tweet_id}/like",
            headers=self._auth_headers(),
            name="POST /v1/tweets/:id/like",
        )
