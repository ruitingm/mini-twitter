#!/usr/bin/env bash

# Seed script for Mini-Twitter baseline testing.
# What it does:
# 1. Creates a pool of test users
# 2. Logs them in and captures token + user_id
# 3. Builds a simple follow graph
# 4. Creates initial tweets for each user
# 5. Writes reusable credentials to testing/test_users.json
#
# This version is safer for AWS because:
# - it throttles requests to avoid rate limiting
# - it validates API responses
# - it fails loudly when tweet creation/login fails

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

# Start smaller for AWS baseline seeding
USER_COUNT="${USER_COUNT:-20}"
FOLLOWS_PER_USER="${FOLLOWS_PER_USER:-5}"
TWEETS_PER_USER="${TWEETS_PER_USER:-5}"
PASSWORD="${PASSWORD:-password123}"

# Throttling controls (seconds)
REGISTER_SLEEP="${REGISTER_SLEEP:-1}"
LOGIN_SLEEP="${LOGIN_SLEEP:-1}"
FOLLOW_SLEEP="${FOLLOW_SLEEP:-1}"
TWEET_SLEEP="${TWEET_SLEEP:-1}"

OUT_DIR="testing"
OUT_FILE="$OUT_DIR/test_users.json"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing dependency: $1"
    exit 1
  }
}

need curl
need jq

mkdir -p "$OUT_DIR"

RUN_ID="$(date +%s)"
USER_DATA_FILE="$(mktemp)"

echo "Using BASE_URL=$BASE_URL"
echo "USER_COUNT=$USER_COUNT"
echo "FOLLOWS_PER_USER=$FOLLOWS_PER_USER"
echo "TWEETS_PER_USER=$TWEETS_PER_USER"
echo "REGISTER_SLEEP=$REGISTER_SLEEP"
echo "LOGIN_SLEEP=$LOGIN_SLEEP"
echo "FOLLOW_SLEEP=$FOLLOW_SLEEP"
echo "TWEET_SLEEP=$TWEET_SLEEP"
echo

request_json() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local token="${4:-}"

  local url="${BASE_URL}${path}"
  local -a args
  args=(-sS -X "$method" "$url")

  if [[ -n "$body" ]]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi

  if [[ -n "$token" ]]; then
    args+=(-H "Authorization: Bearer ${token}")
  fi

  curl "${args[@]}"
}

# ------------------------------------------------------------
# Step 1: Create users and log them in
# ------------------------------------------------------------
echo "== Step 1: Creating users =="

for i in $(seq 1 "$USER_COUNT"); do
  USERNAME="loaduser_${RUN_ID}_$i"
  EMAIL="${USERNAME}@example.com"

  echo "Creating user: $USERNAME"

  REGISTER_RES=$(request_json POST "/v1/auth/register" \
    "{\"username\":\"$USERNAME\",\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")

  REGISTER_TOKEN=$(echo "$REGISTER_RES" | jq -r '.token // empty')
  REGISTER_USER_ID=$(echo "$REGISTER_RES" | jq -r '.user.id // empty')
  REGISTER_ERROR=$(echo "$REGISTER_RES" | jq -r '.error // empty')

  if [[ -n "$REGISTER_ERROR" ]]; then
    echo "Register failed for $USERNAME"
    echo "$REGISTER_RES" | jq .
    exit 1
  fi

  if [[ -z "$REGISTER_TOKEN" || -z "$REGISTER_USER_ID" ]]; then
    echo "Register response missing token/user.id for $USERNAME"
    echo "$REGISTER_RES" | jq .
    exit 1
  fi

  sleep "$REGISTER_SLEEP"

  LOGIN_RES=$(request_json POST "/v1/auth/login" \
    "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")

  TOKEN=$(echo "$LOGIN_RES" | jq -r '.token // empty')
  USER_ID=$(echo "$LOGIN_RES" | jq -r '.user.id // empty')
  LOGIN_ERROR=$(echo "$LOGIN_RES" | jq -r '.error // empty')

  if [[ -n "$LOGIN_ERROR" ]]; then
    echo "Login failed for $USERNAME"
    echo "$LOGIN_RES" | jq .
    exit 1
  fi

  if [[ -z "$TOKEN" || -z "$USER_ID" ]]; then
    echo "Failed to parse token/user_id for $USERNAME"
    echo "$LOGIN_RES" | jq .
    exit 1
  fi

  jq -n \
    --arg username "$USERNAME" \
    --arg email "$EMAIL" \
    --arg password "$PASSWORD" \
    --arg token "$TOKEN" \
    --arg user_id "$USER_ID" \
    '{
      username: $username,
      email: $email,
      password: $password,
      token: $token,
      user_id: $user_id
    }' >> "$USER_DATA_FILE"

  echo "Created $USERNAME with user_id=$USER_ID"

  sleep "$LOGIN_SLEEP"
done

# Convert newline-delimited JSON to JSON array
jq -s '.' "$USER_DATA_FILE" > "$OUT_FILE"

echo
echo "Saved user credentials to $OUT_FILE"
echo

# ------------------------------------------------------------
# Step 2: Build follow graph
# Circular pattern:
# user_i follows next K users
# ------------------------------------------------------------
echo "== Step 2: Building follow graph =="

for i in $(seq 0 $((USER_COUNT - 1))); do
  FOLLOWER_TOKEN=$(jq -r ".[$i].token" "$OUT_FILE")

  for step in $(seq 1 "$FOLLOWS_PER_USER"); do
    FOLLOWEE_INDEX=$(( (i + step) % USER_COUNT ))
    FOLLOWEE_ID=$(jq -r ".[$FOLLOWEE_INDEX].user_id" "$OUT_FILE")

    FOLLOW_RES=$(request_json POST "/v1/users/$FOLLOWEE_ID/follow" "" "$FOLLOWER_TOKEN")
    FOLLOW_ERROR=$(echo "$FOLLOW_RES" | jq -r '.error // empty' 2>/dev/null || true)

    if [[ -n "$FOLLOW_ERROR" ]]; then
      echo "Follow failed: user index $i -> followee index $FOLLOWEE_INDEX"
      echo "$FOLLOW_RES" | jq .
      exit 1
    fi

    sleep "$FOLLOW_SLEEP"
  done
done

echo "Follow graph created."
echo

# ------------------------------------------------------------
# Step 3: Create initial tweets for each user
# ------------------------------------------------------------
echo "== Step 3: Creating initial tweets =="

for i in $(seq 0 $((USER_COUNT - 1))); do
  TOKEN=$(jq -r ".[$i].token" "$OUT_FILE")
  USERNAME=$(jq -r ".[$i].username" "$OUT_FILE")

  for t in $(seq 1 "$TWEETS_PER_USER"); do
    CONTENT="seed tweet $t from $USERNAME at run $RUN_ID"

    TWEET_RES=$(request_json POST "/v1/tweets" \
      "{\"content\":\"$CONTENT\"}" "$TOKEN")

    TWEET_ID=$(echo "$TWEET_RES" | jq -r '.id // empty')
    TWEET_ERROR=$(echo "$TWEET_RES" | jq -r '.error // empty')

    if [[ -n "$TWEET_ERROR" ]]; then
      echo "Tweet creation failed for $USERNAME"
      echo "$TWEET_RES" | jq .
      exit 1
    fi

    if [[ -z "$TWEET_ID" ]]; then
      echo "Tweet response missing id for $USERNAME"
      echo "$TWEET_RES" | jq .
      exit 1
    fi

    sleep "$TWEET_SLEEP"
  done

  echo "Created $TWEETS_PER_USER tweets for $USERNAME"
done

echo
echo "Seed completed successfully."
echo "Credentials file: $OUT_FILE"
echo
echo "Quick sanity checks:"
echo "1) jq 'length' $OUT_FILE"
echo "2) Use one token to query /v1/timeline/home"
echo "3) Check DB counts for users, tweets, follows"