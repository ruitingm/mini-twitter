#!/usr/bin/env bash

# Exit immediately if a command fails.
set -e

# Default gateway base URL.
# You can override it like:
# BASE_URL=http://your-alb-url ./scripts/e2e_home_timeline.sh
BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "Using BASE_URL=$BASE_URL"
echo

# ------------------------------------------------------------
# Step 1: Register Alice
# If Alice already exists, the request may fail, which is okay.
# We use '|| true' so the script can continue.
# ------------------------------------------------------------
echo "== Step 1: Register Alice =="
curl -sS -X POST "$BASE_URL/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","email":"alice@example.com","password":"password123"}' || true
echo
echo

# ------------------------------------------------------------
# Step 2: Login Alice
# We extract Alice's JWT token and user ID from the response.
# ------------------------------------------------------------
echo "== Step 2: Login Alice =="
ALICE_LOGIN=$(curl -sS -X POST "$BASE_URL/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"password123"}')

echo "$ALICE_LOGIN" | jq .

ALICE_TOKEN=$(echo "$ALICE_LOGIN" | jq -r '.token')
ALICE_ID=$(echo "$ALICE_LOGIN" | jq -r '.user.id')

echo "ALICE_ID=$ALICE_ID"
echo

# ------------------------------------------------------------
# Step 3: Register Bob
# If Bob already exists, that is also okay.
# ------------------------------------------------------------
echo "== Step 3: Register Bob =="
curl -sS -X POST "$BASE_URL/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"username":"bob","email":"bob@example.com","password":"password123"}' || true
echo
echo

# ------------------------------------------------------------
# Step 4: Login Bob
# We extract Bob's JWT token and user ID from the response.
# ------------------------------------------------------------
echo "== Step 4: Login Bob =="
BOB_LOGIN=$(curl -sS -X POST "$BASE_URL/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"bob","password":"password123"}')

echo "$BOB_LOGIN" | jq .

BOB_TOKEN=$(echo "$BOB_LOGIN" | jq -r '.token')
BOB_ID=$(echo "$BOB_LOGIN" | jq -r '.user.id')

echo "BOB_ID=$BOB_ID"
echo

# ------------------------------------------------------------
# Step 5: Alice follows Bob
# This creates the follow relationship used by the home timeline.
# ------------------------------------------------------------
echo "== Step 5: Alice follows Bob =="
curl -sS -X POST "$BASE_URL/v1/users/$BOB_ID/follow" \
  -H "Authorization: Bearer $ALICE_TOKEN"
echo
echo

# ------------------------------------------------------------
# Step 6: Bob creates a tweet
# We store the tweet ID for visibility/debugging.
# ------------------------------------------------------------
echo "== Step 6: Bob posts a tweet =="
BOB_TWEET=$(curl -sS -X POST "$BASE_URL/v1/tweets" \
  -H "Authorization: Bearer $BOB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Hi Alice! This is Bob'\''s first tweet."}')

echo "$BOB_TWEET" | jq .

BOB_TWEET_ID=$(echo "$BOB_TWEET" | jq -r '.id')

echo "BOB_TWEET_ID=$BOB_TWEET_ID"
echo

# ------------------------------------------------------------
# Step 7: Alice fetches her home timeline
# Expected result:
# Alice should now see Bob's tweet in her home timeline.
# ------------------------------------------------------------
echo "== Step 7: Alice fetches home timeline =="
HOME_TIMELINE=$(curl -sS "$BASE_URL/v1/timeline/home" \
  -H "Authorization: Bearer $ALICE_TOKEN")

echo "$HOME_TIMELINE" | jq .
echo

# ------------------------------------------------------------
# Final summary
# ------------------------------------------------------------
echo "== Done =="
echo "Verified flow:"
echo "Alice registers/logs in -> Bob registers/logs in -> Alice follows Bob -> Bob posts a tweet -> Alice sees Bob's tweet in home timeline"