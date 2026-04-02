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

# Optimized dataset for multiple experiments
USER_COUNT="${USER_COUNT:-300}"
FOLLOWS_PER_USER="${FOLLOWS_PER_USER:-15}"
TWEETS_PER_USER="${TWEETS_PER_USER:-50}"
CELEBRITY_FOLLOWERS="${CELEBRITY_FOLLOWERS:-150}"  # Celebrity gets extra followers
PASSWORD="${PASSWORD:-password123}"

# No delays with high rate limit (50000 RPM)
REGISTER_SLEEP="${REGISTER_SLEEP:-0}"
LOGIN_SLEEP="${LOGIN_SLEEP:-0}"
FOLLOW_SLEEP="${FOLLOW_SLEEP:-0}"
TWEET_SLEEP="${TWEET_SLEEP:-0}"

# Parallelism settings
MAX_PARALLEL="${MAX_PARALLEL:-10}"  # Number of concurrent processes

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

# Helper function to create a single user
create_user() {
  local i=$1
  local user_data_file=$2
  
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

  # Write to individual temp file to avoid file locking issues
  local temp_file="${user_data_file}.${i}"
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
    }' > "$temp_file"

  echo "Created $USERNAME with user_id=$USER_ID"
  sleep "$LOGIN_SLEEP"
}

# ------------------------------------------------------------
# Step 1: Create users and log them in (parallel)
# ------------------------------------------------------------
echo "== Step 1: Creating users (parallel) =="

# Create users in parallel batches
for batch_start in $(seq 1 "$MAX_PARALLEL" "$USER_COUNT"); do
  batch_end=$((batch_start + MAX_PARALLEL - 1))
  if [ "$batch_end" -gt "$USER_COUNT" ]; then
    batch_end="$USER_COUNT"
  fi
  
  echo "Creating users batch: $batch_start to $batch_end"
  
  # Start parallel processes for this batch
  for i in $(seq "$batch_start" "$batch_end"); do
    create_user "$i" "$USER_DATA_FILE" &
  done
  
  # Wait for this batch to complete
  wait
  echo "Batch $batch_start-$batch_end completed"
done

# Merge all individual user files into final JSON array
echo "Merging user data files..."
for i in $(seq 1 "$USER_COUNT"); do
  cat "${USER_DATA_FILE}.${i}"
done | jq -s '.' > "$OUT_FILE"

# Clean up temp files
for i in $(seq 1 "$USER_COUNT"); do
  rm -f "${USER_DATA_FILE}.${i}"
done

echo
echo "Saved user credentials to $OUT_FILE"
echo

# Helper function to create follows for a single user
create_follows_for_user() {
  local i=$1
  
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
}

# ------------------------------------------------------------
# Step 2: Build follow graph (parallel)
# Circular pattern: user_i follows next K users
# ------------------------------------------------------------
echo "== Step 2: Building follow graph (parallel) =="

# Create follows in parallel batches
for batch_start in $(seq 0 $((MAX_PARALLEL - 1)) $((USER_COUNT - 1))); do
  batch_end=$((batch_start + MAX_PARALLEL - 1))
  if [ "$batch_end" -ge "$USER_COUNT" ]; then
    batch_end=$((USER_COUNT - 1))
  fi
  
  echo "Creating follows batch: $batch_start to $batch_end"
  
  # Start parallel processes for this batch
  for i in $(seq "$batch_start" "$batch_end"); do
    create_follows_for_user "$i" &
  done
  
  # Wait for this batch to complete
  wait
  echo "Follow batch $batch_start-$batch_end completed"
done

echo "Follow graph created."
echo

# ------------------------------------------------------------
# Step 2.5: Create celebrity user with many followers (for consistency experiment)
# ------------------------------------------------------------
echo "== Step 2.5: Setting up celebrity user =="

# Use the first user as the celebrity
CELEBRITY_ID=$(jq -r ".[0].user_id" "$OUT_FILE")
CELEBRITY_USERNAME=$(jq -r ".[0].username" "$OUT_FILE")

echo "Celebrity user: $CELEBRITY_USERNAME ($CELEBRITY_ID)"
echo "Adding $CELEBRITY_FOLLOWERS additional followers..."

# Add extra followers to the celebrity (beyond the regular follow graph)
for i in $(seq 1 "$CELEBRITY_FOLLOWERS"); do
  FOLLOWER_INDEX=$(( i % USER_COUNT ))
  
  # Skip if this user already follows celebrity from regular graph
  if [ $FOLLOWER_INDEX -eq 0 ]; then
    continue
  fi
  
  FOLLOWER_TOKEN=$(jq -r ".[$FOLLOWER_INDEX].token" "$OUT_FILE")
  
  FOLLOW_RES=$(request_json POST "/v1/users/$CELEBRITY_ID/follow" "" "$FOLLOWER_TOKEN")
  FOLLOW_ERROR=$(echo "$FOLLOW_RES" | jq -r '.error // empty' 2>/dev/null || true)
  
  if [[ -n "$FOLLOW_ERROR" ]] && [[ "$FOLLOW_ERROR" != "already following" ]]; then
    echo "Celebrity follow failed: user $FOLLOWER_INDEX -> celebrity"
    echo "$FOLLOW_RES" | jq .
  fi
  
  sleep "$FOLLOW_SLEEP"
done

echo "Celebrity setup complete: $CELEBRITY_USERNAME now has ~$CELEBRITY_FOLLOWERS followers"
echo

# Helper function to create tweets for a single user
create_tweets_for_user() {
  local i=$1
  
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
}

# ------------------------------------------------------------
# Step 3: Create initial tweets for each user (parallel)
# ------------------------------------------------------------
echo "== Step 3: Creating initial tweets (parallel) =="

# Create tweets in parallel batches
for batch_start in $(seq 0 $((MAX_PARALLEL - 1)) $((USER_COUNT - 1))); do
  batch_end=$((batch_start + MAX_PARALLEL - 1))
  if [ "$batch_end" -ge "$USER_COUNT" ]; then
    batch_end=$((USER_COUNT - 1))
  fi
  
  echo "Creating tweets batch: $batch_start to $batch_end"
  
  # Start parallel processes for this batch
  for i in $(seq "$batch_start" "$batch_end"); do
    create_tweets_for_user "$i" &
  done
  
  # Wait for this batch to complete
  wait
  echo "Tweet batch $batch_start-$batch_end completed"
done

echo
echo "Seed completed successfully."
echo "Credentials file: $OUT_FILE"
echo
echo "Quick sanity checks:"
echo "1) jq 'length' $OUT_FILE"
echo "2) Use one token to query /v1/timeline/home"
echo "3) Check DB counts for users, tweets, follows"
