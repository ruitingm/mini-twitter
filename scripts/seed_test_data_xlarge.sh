#!/usr/bin/env bash
# Seed script for Experiment 2 high-contention testing.
# Scales up from the large dataset (300 users) to 500 users proportionally.
#
# Dataset size:
#   - 500 users
#   - 25 follows per user  (~7,500 total follows, 5/3x of large)
#   - 80 tweets per user   (~40,000 total tweets, 5/3x of large)
#   - 1 celebrity user with ~250 followers
#
# Expected runtime: ~10-15 minutes (parallelized)

export USER_COUNT=500
export FOLLOWS_PER_USER=25
export TWEETS_PER_USER=80
export CELEBRITY_FOLLOWERS=250

exec "$(dirname "$0")/seed_test_data_large.sh"
