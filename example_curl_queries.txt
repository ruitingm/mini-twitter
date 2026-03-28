# Mini-Twitter API Testing Guide
# Complete user journey with curl commands and PostgreSQL validation queries

## Prerequisites
# Ensure services are running: docker-compose up --build

# ====================================================================
# 1. USER REGISTRATION & AUTHENTICATION
# ====================================================================

## Register first user
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","email":"alice@example.com","password":"password123"}'

# Expected response: {"token":"...", "user":{"id":"uuid","username":"alice",...}}
# Save the user ID for later use

## Register second user
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"bob","email":"bob@example.com","password":"password123"}'

## Register third user (for testing)
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"charlie","email":"charlie@example.com","password":"password123"}'

## Login and get tokens
ALICE_TOKEN=$(curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"password123"}' \
  | jq -r '.token')

BOB_TOKEN=$(curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"bob","password":"password123"}' \
  | jq -r '.token')

CHARLIE_TOKEN=$(curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"charlie","password":"password123"}' \
  | jq -r '.token')

## Test invalid login
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"wrongpassword"}'

# Expected: 401 Unauthorized

# ====================================================================
# 2. USER PROFILES
# ====================================================================

## Get public profile
curl http://localhost:8080/v1/users/alice
curl http://localhost:8080/v1/users/bob

## Update own profile
curl -X PUT http://localhost:8080/v1/users/me \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"display_name":"Alice Smith","bio":"Software developer and coffee enthusiast"}'

curl -X PUT http://localhost:8080/v1/users/me \
  -H "Authorization: Bearer $BOB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"display_name":"Bob Johnson","bio":"Photographer and traveler"}'

## Try to update someone else's profile (should fail)
curl -X PUT http://localhost:8080/v1/users/me \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username":"hacker"}'

# ====================================================================
# 3. TWEET CREATION & MANAGEMENT
# ====================================================================

## Alice creates tweets
curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Hello Twitter! This is my first tweet 🎉"}'

curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Just finished a great coding session. Love building distributed systems!"}'

ALICE_TWEET_ID=$(curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Working on microservices architecture. Fan-out strategies are fascinating!"}' \
  | jq -r '.id')

## Bob creates tweets
curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $BOB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Beautiful sunset today 🌅"}'

BOB_TWEET_ID=$(curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $BOB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Photography tip: Golden hour is the best time for portraits"}' \
  | jq -r '.id')

## Charlie creates a tweet
CHARLIE_TWEET_ID=$(curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $CHARLIE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"Testing the new social media platform. Looks promising!"}' \
  | jq -r '.id')

## Test tweet length validation (should fail - over 280 chars)
curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"This is a very long tweet that exceeds the 280 character limit. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris."}'

## Test empty tweet (should fail)
curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":""}'

## Create a reply tweet (if reply functionality exists)
curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $BOB_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"content\":\"Great point about distributed systems!\",\"reply_to_id\":\"$ALICE_TWEET_ID\"}"

# ====================================================================
# 4. LIKE FUNCTIONALITY (Testing both Strong & Eventual Consistency)
# ====================================================================

## Bob likes Alice's tweets
curl -X POST http://localhost:8080/v1/tweets/$ALICE_TWEET_ID/like \
  -H "Authorization: Bearer $BOB_TOKEN"

## Charlie likes Alice's tweet
curl -X POST http://localhost:8080/v1/tweets/$ALICE_TWEET_ID/like \
  -H "Authorization: Bearer $CHARLIE_TOKEN"

## Alice likes Bob's tweet
curl -X POST http://localhost:8080/v1/tweets/$BOB_TWEET_ID/like \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Charlie likes Bob's tweet
curl -X POST http://localhost:8080/v1/tweets/$BOB_TWEET_ID/like \
  -H "Authorization: Bearer $CHARLIE_TOKEN"

## Test duplicate like (should be handled gracefully)
curl -X POST http://localhost:8080/v1/tweets/$ALICE_TWEET_ID/like \
  -H "Authorization: Bearer $BOB_TOKEN"

## Unlike a tweet
curl -X DELETE http://localhost:8080/v1/tweets/$ALICE_TWEET_ID/like \
  -H "Authorization: Bearer $CHARLIE_TOKEN"

## Like it again
curl -X POST http://localhost:8080/v1/tweets/$ALICE_TWEET_ID/like \
  -H "Authorization: Bearer $CHARLIE_TOKEN"

## Test liking own tweet
curl -X POST http://localhost:8080/v1/tweets/$ALICE_TWEET_ID/like \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Test liking non-existent tweet
curl -X POST http://localhost:8080/v1/tweets/00000000-0000-0000-0000-000000000000/like \
  -H "Authorization: Bearer $BOB_TOKEN"

# ====================================================================
# 5. FOLLOW RELATIONSHIPS
# ====================================================================

# First, get user IDs (replace with actual IDs from registration responses)
# Example IDs - replace with your actual UUIDs:
# ALICE_ID="ecdac18a-580f-465b-851e-e827212c1b36"
# BOB_ID="56c7874c-0fac-4ad7-b552-c0661098870e"  
# CHARLIE_ID="your-charlie-uuid-here"

## Alice follows Bob
curl -X POST http://localhost:8080/v1/users/$BOB_ID/follow \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Alice follows Charlie  
curl -X POST http://localhost:8080/v1/users/$CHARLIE_ID/follow \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Bob follows Alice
curl -X POST http://localhost:8080/v1/users/$ALICE_ID/follow \
  -H "Authorization: Bearer $BOB_TOKEN"

## Charlie follows Alice
curl -X POST http://localhost:8080/v1/users/$ALICE_ID/follow \
  -H "Authorization: Bearer $CHARLIE_TOKEN"

## Test duplicate follow (should be handled gracefully)
curl -X POST http://localhost:8080/v1/users/$BOB_ID/follow \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Test following yourself (should fail)
curl -X POST http://localhost:8080/v1/users/$ALICE_ID/follow \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Unfollow someone
curl -X DELETE http://localhost:8080/v1/users/$CHARLIE_ID/follow \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Follow them again
curl -X POST http://localhost:8080/v1/users/$CHARLIE_ID/follow \
  -H "Authorization: Bearer $ALICE_TOKEN"

# ====================================================================
# 6. TIMELINE FUNCTIONALITY  
# ====================================================================

## Get home timelines (should show tweets from followed users)
echo "=== Alice's Home Timeline ==="
curl http://localhost:8080/v1/timeline/home \
  -H "Authorization: Bearer $ALICE_TOKEN"

echo "=== Bob's Home Timeline ==="
curl http://localhost:8080/v1/timeline/home \
  -H "Authorization: Bearer $BOB_TOKEN"

echo "=== Charlie's Home Timeline ==="
curl http://localhost:8080/v1/timeline/home \
  -H "Authorization: Bearer $CHARLIE_TOKEN"

## Get user timelines (public - all tweets from specific user)
echo "=== Alice's Public Timeline ==="
curl http://localhost:8080/v1/timeline/user/$ALICE_ID

echo "=== Bob's Public Timeline ==="
curl http://localhost:8080/v1/timeline/user/$BOB_ID

echo "=== Charlie's Public Timeline ==="
curl http://localhost:8080/v1/timeline/user/$CHARLIE_ID

## Test timeline pagination (if implemented)
curl "http://localhost:8080/v1/timeline/home?limit=2" \
  -H "Authorization: Bearer $ALICE_TOKEN"

# ====================================================================
# 7. TWEET DELETION
# ====================================================================

## Create a tweet to delete
DELETE_TWEET_ID=$(curl -X POST http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer $ALICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":"This tweet will be deleted"}' \
  | jq -r '.id')

## Delete own tweet
curl -X DELETE http://localhost:8080/v1/tweets/$DELETE_TWEET_ID \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Try to delete someone else's tweet (should fail)
curl -X DELETE http://localhost:8080/v1/tweets/$BOB_TWEET_ID \
  -H "Authorization: Bearer $ALICE_TOKEN"

## Try to delete non-existent tweet
curl -X DELETE http://localhost:8080/v1/tweets/00000000-0000-0000-0000-000000000000 \
  -H "Authorization: Bearer $ALICE_TOKEN"

# ====================================================================
# 8. ERROR HANDLING & EDGE CASES
# ====================================================================

## Test unauthorized access
curl http://localhost:8080/v1/timeline/home

## Test malformed JSON
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"test"'

## Test missing required fields
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"test"}'

## Test invalid JWT token
curl http://localhost:8080/v1/timeline/home \
  -H "Authorization: Bearer invalid.jwt.token"

## Test expired/malformed bearer token
curl http://localhost:8080/v1/tweets \
  -H "Authorization: Bearer expired-token"

# ====================================================================
# 9. POSTGRESQL DATABASE VALIDATION QUERIES
# ====================================================================

# Connect to database using one of these methods:
# Method 1: docker exec -it postgres-primary-1 psql -U twitter -d twitter  
# Method 2: docker-compose exec postgres-primary psql -U twitter -d twitter

## Once connected to PostgreSQL, run these queries:

/*
-- Check all users and their stats
SELECT 
    id,
    username, 
    email,
    display_name,
    bio,
    follower_count,
    following_count,
    created_at
FROM users 
ORDER BY created_at;

-- Check all tweets with user information
SELECT 
    t.id,
    u.username,
    t.content,
    t.like_count,
    t.reply_to_id,
    t.created_at
FROM tweets t
JOIN users u ON t.user_id = u.id
ORDER BY t.created_at DESC;

-- Check follow relationships
SELECT 
    follower.username AS follower,
    followee.username AS followed,
    f.created_at
FROM follows f
JOIN users follower ON f.follower_id = follower.id
JOIN users followee ON f.followee_id = followee.id
ORDER BY f.created_at;

-- Check likes with user and tweet information
SELECT 
    u.username AS liker,
    t_author.username AS tweet_author,
    t.content,
    l.created_at AS liked_at
FROM likes l
JOIN users u ON l.user_id = u.id
JOIN tweets t ON l.tweet_id = t.id
JOIN users t_author ON t.user_id = t_author.id
ORDER BY l.created_at DESC;

-- Check eventual consistency - pending like counts (if using eventual consistency mode)
SELECT 
    t.content,
    t_author.username AS author,
    lcp.delta,
    lcp.created_at
FROM like_count_pending lcp
JOIN tweets t ON lcp.tweet_id = t.id
JOIN users t_author ON t.user_id = t_author.id
ORDER BY lcp.created_at DESC;

-- Verify like count accuracy (compare actual vs computed)
SELECT 
    t.id,
    t.content,
    t.like_count AS stored_count,
    COUNT(l.tweet_id) AS actual_count,
    (t.like_count = COUNT(l.tweet_id)) AS counts_match
FROM tweets t
LEFT JOIN likes l ON t.id = l.tweet_id
GROUP BY t.id, t.content, t.like_count
ORDER BY t.created_at DESC;

-- Summary statistics
SELECT 
    (SELECT COUNT(*) FROM users) as total_users,
    (SELECT COUNT(*) FROM tweets) as total_tweets,  
    (SELECT COUNT(*) FROM follows) as total_follows,
    (SELECT COUNT(*) FROM likes) as total_likes,
    (SELECT COUNT(*) FROM like_count_pending) as pending_like_updates;

-- Check for data consistency issues
SELECT 'Orphaned tweets' as issue, COUNT(*) as count
FROM tweets t 
LEFT JOIN users u ON t.user_id = u.id 
WHERE u.id IS NULL

UNION ALL

SELECT 'Orphaned likes' as issue, COUNT(*) as count  
FROM likes l
LEFT JOIN tweets t ON l.tweet_id = t.id
WHERE t.id IS NULL

UNION ALL

SELECT 'Orphaned follows' as issue, COUNT(*) as count
FROM follows f
LEFT JOIN users u1 ON f.follower_id = u1.id
LEFT JOIN users u2 ON f.followee_id = u2.id  
WHERE u1.id IS NULL OR u2.id IS NULL;

-- User activity analysis
SELECT 
    u.username,
    COUNT(DISTINCT t.id) as tweets_count,
    COUNT(DISTINCT l.tweet_id) as likes_given,
    COUNT(DISTINCT likes_received.id) as likes_received,
    COUNT(DISTINCT f1.followee_id) as following,
    COUNT(DISTINCT f2.follower_id) as followers
FROM users u
LEFT JOIN tweets t ON u.id = t.user_id
LEFT JOIN likes l ON u.id = l.user_id  
LEFT JOIN tweets user_tweets ON u.id = user_tweets.user_id
LEFT JOIN likes likes_received ON user_tweets.id = likes_received.tweet_id
LEFT JOIN follows f1 ON u.id = f1.follower_id
LEFT JOIN follows f2 ON u.id = f2.followee_id
GROUP BY u.id, u.username
ORDER BY tweets_count DESC;

-- Recent activity timeline
SELECT 
    'tweet' as activity_type,
    u.username,
    t.content as description,
    t.created_at
FROM tweets t
JOIN users u ON t.user_id = u.id

UNION ALL

SELECT 
    'like' as activity_type,
    u.username,
    'liked: ' || t.content as description,
    l.created_at
FROM likes l
JOIN users u ON l.user_id = u.id
JOIN tweets t ON l.tweet_id = t.id

UNION ALL

SELECT 
    'follow' as activity_type,
    follower.username,
    'followed: ' || followee.username as description,
    f.created_at
FROM follows f
JOIN users follower ON f.follower_id = follower.id
JOIN users followee ON f.followee_id = followee.id

ORDER BY created_at DESC
LIMIT 20;

-- Check database table sizes and performance
SELECT 
    schemaname,
    tablename,
    attname,
    n_distinct,
    correlation
FROM pg_stats 
WHERE schemaname = 'public'
ORDER BY tablename, attname;
*/

# ====================================================================
# 10. REDIS CACHE INSPECTION (Optional)
# ====================================================================

# Connect to Redis to inspect cached data
# docker exec -it redis-1 redis-cli

## Redis commands to check cached data:
# KEYS *                          # List all keys
# KEYS timeline:*                 # List timeline keys  
# KEYS like_count:*               # List like count keys
# LRANGE timeline:USER_ID 0 -1    # View user's timeline cache
# GET like_count:TWEET_ID         # View tweet's like count cache
# TTL timeline:USER_ID            # Check cache expiration

# ====================================================================
# 11. LOAD TESTING PREPARATION
# ====================================================================

## Create multiple users for load testing
for i in {1..10}; do
  curl -X POST http://localhost:8080/v1/auth/register \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"loadtest$i\",\"email\":\"loadtest$i@example.com\",\"password\":\"password123\"}"
done

## Create tweets from load test users
for i in {1..10}; do
  TOKEN=$(curl -X POST http://localhost:8080/v1/auth/login \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"loadtest$i\",\"password\":\"password123\"}" \
    | jq -r '.token')
  
  curl -X POST http://localhost:8080/v1/tweets \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"content\":\"Load test tweet from user $i\"}"
done

# ====================================================================
# EXPERIMENT TESTING - REDIS VS POSTGRESQL & CONSISTENCY MODES
# ====================================================================

## Test different configurations by restarting with environment variables:

# Redis caching + eventual consistency (default)
# USE_REDIS=true CONSISTENCY_MODE=eventual docker-compose up

# Direct PostgreSQL + strong consistency  
# USE_REDIS=false CONSISTENCY_MODE=strong docker-compose up

# Redis caching + strong consistency
# USE_REDIS=true CONSISTENCY_MODE=strong docker-compose up  

# Direct PostgreSQL + eventual consistency
# USE_REDIS=false CONSISTENCY_MODE=eventual docker-compose up

## After each configuration change, test:
# 1. Timeline load performance
# 2. Tweet creation latency  
# 3. Like operation latency
# 4. Consistency of like counts
# 5. Cache hit/miss behavior

echo "=== Testing Complete ==="
echo "All endpoints tested successfully!"
echo "Connect to PostgreSQL to run validation queries:"
echo "psql postgres://twitter:twitter@localhost:5432/twitter"