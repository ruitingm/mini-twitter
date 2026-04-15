import json
import os
import random
import time
from locust import HttpUser, task, between

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


class MiniTwitterUser(HttpUser):
    """
    Baseline Locust user for Mini-Twitter.

    Workload design:
    - Mostly read-heavy
    - Occasionally create tweets
    - Uses pre-seeded users/tokens
    """

    # wait_time = between(1, 3)
    wait_time = between(1, 2)

    def on_start(self):
        # Each virtual user randomly chooses one seeded account
        self.user = random.choice(TEST_USERS)
        self.token = self.user["token"]
        self.user_id = self.user["user_id"]

    @task(3)
    def get_home_timeline(self):
        """
        Main read workload for fan-out experiments.
        """
        self.client.get(
            "/v1/timeline/home",
            headers={"Authorization": f"Bearer {self.token}"},
            name="GET /v1/timeline/home",
        )

    @task(2)
    def get_user_timeline(self):
        """
        Secondary read workload.
        """
        self.client.get(
            f"/v1/timeline/user/{self.user_id}",
            headers={"Authorization": f"Bearer {self.token}"},
            name="GET /v1/timeline/user/:id",
        )

    @task(1)
    def post_tweet(self):
        """
        Main write workload for fan-out experiments.
        """
        payload = {
            "content": f"locust tweet from {self.user['username']} at {int(time.time() * 1000)}"
        }

        self.client.post(
            "/v1/tweets",
            headers={
                "Authorization": f"Bearer {self.token}",
                "Content-Type": "application/json",
            },
            json=payload,
            name="POST /v1/tweets",
        )


class LikeContendingUser(HttpUser):
    """
    Experiment 2: Strong vs. Eventual Consistency

    Like-heavy workload to stress-test like count consistency.
    Many virtual users concurrently like the same hot tweets to create write contention.

    Strong mode  (CONSISTENCY_MODE=strong):   each like = 1 synchronous DB write
    Eventual mode (CONSISTENCY_MODE=eventual): each like = Redis INCR, flushed every 30s

    Run with:
        locust -f locustfile.py LikeContendingUser --host=http://localhost:8080
    """

    wait_time = between(0.5, 1.5)

    def on_start(self):
        self.user = random.choice(TEST_USERS)
        self.token = self.user["token"]
        self.user_id = self.user["user_id"]
        self.tweet_ids = []     # hot tweet IDs available to like
        self.liked_ids = set()  # tweets this virtual user has already liked (to avoid duplicate-key errors)
        self._fetch_tweet_ids()

    def _fetch_tweet_ids(self):
        """Populate tweet_ids from home timeline so all users target the same hot tweets."""
        resp = self.client.get(
            "/v1/timeline/home",
            headers={"Authorization": f"Bearer {self.token}"},
            name="GET /v1/timeline/home (setup)",
        )
        if resp.status_code == 200:
            data = resp.json() or {}
            tweets = data.get("tweets", []) if isinstance(data, dict) else data
            self.tweet_ids = [t["id"] for t in tweets[:20]]

    @task(5)
    def like_tweet(self):
        """
        Primary contention workload: like a random hot tweet.
        Unlikes first if already liked by this user, then re-likes on next invocation.
        This cycling pattern keeps a steady stream of like writes without hitting
        the unique-constraint on (user_id, tweet_id).
        """
        if not self.tweet_ids:
            self._fetch_tweet_ids()
            return

        tweet_id = random.choice(self.tweet_ids)

        if tweet_id in self.liked_ids:
            # Reset: unlike so we can like again next time
            resp = self.client.delete(
                f"/v1/tweets/{tweet_id}/like",
                headers={"Authorization": f"Bearer {self.token}"},
                name="DELETE /v1/tweets/:id/like",
            )
            if resp.status_code == 204:
                self.liked_ids.discard(tweet_id)
        else:
            resp = self.client.post(
                f"/v1/tweets/{tweet_id}/like",
                headers={"Authorization": f"Bearer {self.token}"},
                name="POST /v1/tweets/:id/like",
            )
            if resp.status_code == 204:
                self.liked_ids.add(tweet_id)

    @task(2)
    def read_like_count(self):
        """
        Reader workload: fetch a hot tweet to observe its like_count.
        In eventual mode this returns a stale count until the aggregator flushes.
        Captured separately in Locust charts as GET /v1/tweets/:id.
        """
        if not self.tweet_ids:
            return
        tweet_id = random.choice(self.tweet_ids)
        self.client.get(
            f"/v1/tweets/{tweet_id}",
            name="GET /v1/tweets/:id",
        )

    @task(1)
    def get_home_timeline(self):
        self.client.get(
            "/v1/timeline/home",
            headers={"Authorization": f"Bearer {self.token}"},
            name="GET /v1/timeline/home",
        )