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

    wait_time = between(1, 3)

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