#!/usr/bin/env bash
# ============================================================================
# Experiment 3: Failure Resilience Tests
# ============================================================================
# Orchestrates the full resilience experiment:
#   Phase 1 - BASELINE:         Steady-state load (all components healthy)
#   Phase 2 - REPLICA_FAILURE:  Reboot one PostgreSQL read replica
#   Phase 3 - REPLICA_RECOVERY: Wait for replica to come back online
#   Phase 4 - REDIS_FLUSH:      FLUSHALL on Redis to simulate cache failure
#   Phase 5 - COOLDOWN:         Let system reach new steady state
#
# Prerequisites:
#   - AWS CLI configured with appropriate permissions
#   - Locust installed (pip install locust)
#   - Test data seeded (./scripts/seed_test_data_large.sh)
#   - Infrastructure deployed (terraform apply)
#
# Usage (run from project root):
#   export BASE_URL="http://<alb-dns>"
#   export REDIS_HOST="<elasticache-endpoint>"
#   ./experiments/experiment3/scripts/experiment3_resilience.sh
# ============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration (override via environment variables)
# ---------------------------------------------------------------------------
BASE_URL="${BASE_URL:?BASE_URL must be set to your ALB DNS (e.g. http://mini-twitter-alb-xxx.us-west-2.elb.amazonaws.com)}"
REDIS_HOST="${REDIS_HOST:?REDIS_HOST must be set to your ElastiCache endpoint}"
REDIS_PORT="${REDIS_PORT:-6379}"

# RDS replica to reboot (default matches terraform naming)
REPLICA_ID="${REPLICA_ID:-mini-twitter-postgres-replica2}"

# Locust settings
LOCUST_USERS="${LOCUST_USERS:-50}"
LOCUST_SPAWN_RATE="${LOCUST_SPAWN_RATE:-10}"
LOCUST_FILE="${LOCUST_FILE:-experiments/experiment3/locust/locustfile_resilience.py}"

# Phase durations (seconds)
BASELINE_DURATION="${BASELINE_DURATION:-120}"
FAILURE_DURATION="${FAILURE_DURATION:-120}"
RECOVERY_DURATION="${RECOVERY_DURATION:-120}"
REDIS_FLUSH_DURATION="${REDIS_FLUSH_DURATION:-120}"
COOLDOWN_DURATION="${COOLDOWN_DURATION:-60}"

# Output directory
RESULTS_DIR="experiments/experiment3/results/$(date +%Y%m%d_%H%M%S)"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log() {
    local phase="$1"
    local msg="$2"
    local ts
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    echo "[$ts] [$phase] $msg"
    echo "$ts,$phase,$msg" >> "$RESULTS_DIR/phase_log.csv"
}

cleanup() {
    log "CLEANUP" "Stopping Locust (PID: ${LOCUST_PID:-unknown})"
    if [[ -n "${LOCUST_PID:-}" ]] && kill -0 "$LOCUST_PID" 2>/dev/null; then
        kill "$LOCUST_PID" 2>/dev/null || true
        wait "$LOCUST_PID" 2>/dev/null || true
    fi
    log "CLEANUP" "Done. Results in $RESULTS_DIR"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------
echo "============================================"
echo "  Experiment 3: Failure Resilience Tests"
echo "============================================"
echo ""
echo "Configuration:"
echo "  BASE_URL:       $BASE_URL"
echo "  REDIS_HOST:     $REDIS_HOST"
echo "  REPLICA_ID:     $REPLICA_ID"
echo "  LOCUST_USERS:   $LOCUST_USERS"
echo "  LOCUST_FILE:    $LOCUST_FILE"
echo ""

# Check dependencies
command -v locust >/dev/null 2>&1 || { echo "ERROR: locust not found. Install with: pip install locust"; exit 1; }
command -v aws >/dev/null 2>&1    || { echo "ERROR: aws CLI not found."; exit 1; }

# Check test users exist
if [[ ! -f "testing/test_users.json" ]]; then
    echo "ERROR: testing/test_users.json not found. Run scripts/seed_test_data_large.sh first."
    exit 1
fi

# Verify a token still works
echo "Checking token validity..."
SAMPLE_TOKEN=$(python3 -c "import json; users=json.load(open('testing/test_users.json')); print(users[0]['token'])")
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: Bearer $SAMPLE_TOKEN" \
    "$BASE_URL/v1/timeline/home" 2>/dev/null || echo "000")

if [[ "$HTTP_CODE" == "401" ]]; then
    echo "ERROR: Test tokens have expired. Re-run seed script to generate fresh tokens."
    exit 1
elif [[ "$HTTP_CODE" == "000" ]]; then
    echo "ERROR: Cannot reach $BASE_URL. Check your ALB DNS and connectivity."
    exit 1
fi
echo "Token valid (HTTP $HTTP_CODE). Pre-flight checks passed."
echo ""

# Verify RDS replica exists
echo "Checking RDS replica $REPLICA_ID..."
REPLICA_STATUS=$(aws rds describe-db-instances \
    --db-instance-identifier "$REPLICA_ID" \
    --query 'DBInstances[0].DBInstanceStatus' \
    --output text 2>/dev/null || echo "not-found")

if [[ "$REPLICA_STATUS" == "not-found" ]]; then
    echo "ERROR: RDS replica $REPLICA_ID not found. Check your REPLICA_ID."
    exit 1
fi
echo "Replica status: $REPLICA_STATUS"
echo ""

# Create results directory
mkdir -p "$RESULTS_DIR"
echo "timestamp,phase,event" > "$RESULTS_DIR/phase_log.csv"

# ---------------------------------------------------------------------------
# Start Locust in headless mode (background)
# ---------------------------------------------------------------------------
TOTAL_DURATION=$(( BASELINE_DURATION + FAILURE_DURATION + RECOVERY_DURATION + REDIS_FLUSH_DURATION + COOLDOWN_DURATION ))

log "SETUP" "Starting Locust: $LOCUST_USERS users, spawn rate $LOCUST_SPAWN_RATE, total ${TOTAL_DURATION}s"

locust -f "$LOCUST_FILE" \
    --headless \
    -u "$LOCUST_USERS" \
    -r "$LOCUST_SPAWN_RATE" \
    -t "${TOTAL_DURATION}s" \
    --host="$BASE_URL" \
    --csv="$RESULTS_DIR/locust" \
    --csv-full-history \
    --only-summary 2>&1 | tee "$RESULTS_DIR/locust_output.log" &

LOCUST_PID=$!
log "SETUP" "Locust started (PID: $LOCUST_PID)"

# Give Locust time to spawn all users
RAMP_UP_TIME=$(( LOCUST_USERS / LOCUST_SPAWN_RATE + 5 ))
log "SETUP" "Waiting ${RAMP_UP_TIME}s for user ramp-up..."
sleep "$RAMP_UP_TIME"

# ---------------------------------------------------------------------------
# Phase 1: BASELINE
# ---------------------------------------------------------------------------
log "BASELINE" "Phase 1 started: measuring steady-state performance for ${BASELINE_DURATION}s"
sleep "$BASELINE_DURATION"
log "BASELINE" "Phase 1 complete"

# ---------------------------------------------------------------------------
# Phase 2: REPLICA FAILURE
# ---------------------------------------------------------------------------
log "REPLICA_FAILURE" "Phase 2 started: rebooting RDS replica $REPLICA_ID"

aws rds reboot-db-instance \
    --db-instance-identifier "$REPLICA_ID" \
    2>/dev/null || {
    log "REPLICA_FAILURE" "WARNING: Failed to reboot replica. Continuing with experiment."
}

log "REPLICA_FAILURE" "Reboot command sent. Measuring impact for ${FAILURE_DURATION}s"
sleep "$FAILURE_DURATION"
log "REPLICA_FAILURE" "Phase 2 complete"

# ---------------------------------------------------------------------------
# Phase 3: REPLICA RECOVERY
# ---------------------------------------------------------------------------
log "REPLICA_RECOVERY" "Phase 3 started: waiting for replica to recover"

# Give the reboot time to register before polling status
log "REPLICA_RECOVERY" "Waiting 30s for reboot to take effect..."
sleep 30

# Wait for replica to become available
RECOVERY_START=$(date +%s)
aws rds wait db-instance-available \
    --db-instance-identifier "$REPLICA_ID" \
    2>/dev/null && {
    RECOVERY_END=$(date +%s)
    RECOVERY_TIME=$(( RECOVERY_END - RECOVERY_START + 30 ))
    log "REPLICA_RECOVERY" "Replica available after ${RECOVERY_TIME}s (including 30s reboot delay)"
} || {
    log "REPLICA_RECOVERY" "WARNING: Replica did not become available within timeout"
}

# Measure steady-state after recovery
REMAINING=$(( RECOVERY_DURATION - $(( $(date +%s) - RECOVERY_START )) ))
if [[ $REMAINING -gt 0 ]]; then
    log "REPLICA_RECOVERY" "Measuring post-recovery steady state for ${REMAINING}s"
    sleep "$REMAINING"
fi
log "REPLICA_RECOVERY" "Phase 3 complete"

# ---------------------------------------------------------------------------
# Phase 4: REDIS FLUSH
# ---------------------------------------------------------------------------
log "REDIS_FLUSH" "Phase 4 started: flushing Redis cache"

# Attempt Redis FLUSHALL via multiple methods:
# 1. Direct redis-cli (if on VPC or via bastion)
# 2. ECS Exec into gateway container using netcat (requires enable-execute-command)
# 3. Skip with warning (experiment continues without cache flush)
FLUSH_SUCCESS=false

# Method 1: Direct redis-cli
if command -v redis-cli >/dev/null 2>&1; then
    redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" FLUSHALL 2>/dev/null && {
        log "REDIS_FLUSH" "FLUSHALL executed via redis-cli"
        FLUSH_SUCCESS=true
    } || {
        log "REDIS_FLUSH" "Direct redis-cli failed (likely no VPC access)"
    }
fi

# Method 2: ECS Exec via netcat (Alpine containers include nc but not redis-cli)
if [[ "$FLUSH_SUCCESS" == "false" ]]; then
    ECS_CLUSTER="${ECS_CLUSTER:-mini-twitter-gateway-cluster}"
    ECS_SERVICE="${ECS_SERVICE:-mini-twitter-gateway}"
    ECS_CONTAINER="${ECS_CONTAINER:-mini-twitter-gateway-container}"
    TASK_ARN=$(aws ecs list-tasks --cluster "$ECS_CLUSTER" --service-name "$ECS_SERVICE" --desired-status RUNNING --query 'taskArns[0]' --output text 2>/dev/null || echo "None")

    if [[ -n "$TASK_ARN" && "$TASK_ARN" != "None" ]]; then
        log "REDIS_FLUSH" "Attempting FLUSHALL via ECS Exec (task: $(basename "$TASK_ARN"))..."
        FLUSH_OUTPUT=$(aws ecs execute-command \
            --cluster "$ECS_CLUSTER" \
            --task "$TASK_ARN" \
            --container "$ECS_CONTAINER" \
            --command "/bin/sh -c 'echo FLUSHALL | nc $REDIS_HOST $REDIS_PORT'" \
            --interactive 2>&1)
        if echo "$FLUSH_OUTPUT" | grep -q "+OK"; then
            log "REDIS_FLUSH" "FLUSHALL executed via ECS Exec (+OK)"
            FLUSH_SUCCESS=true
        else
            log "REDIS_FLUSH" "ECS Exec response: $FLUSH_OUTPUT"
        fi
    else
        log "REDIS_FLUSH" "No running gateway tasks found for ECS Exec"
    fi
fi

if [[ "$FLUSH_SUCCESS" == "false" ]]; then
    log "REDIS_FLUSH" "WARNING: Could not flush Redis automatically. Skipping cache flush test."
    log "REDIS_FLUSH" "To run manually: redis-cli -h $REDIS_HOST -p $REDIS_PORT FLUSHALL"
fi

log "REDIS_FLUSH" "Measuring impact for ${REDIS_FLUSH_DURATION}s"
sleep "$REDIS_FLUSH_DURATION"
log "REDIS_FLUSH" "Phase 4 complete"

# ---------------------------------------------------------------------------
# Phase 5: COOLDOWN
# ---------------------------------------------------------------------------
log "COOLDOWN" "Phase 5 started: cooldown for ${COOLDOWN_DURATION}s"
sleep "$COOLDOWN_DURATION"
log "COOLDOWN" "Phase 5 complete"

# ---------------------------------------------------------------------------
# Results Summary
# ---------------------------------------------------------------------------
echo ""
echo "============================================"
echo "  Experiment 3 Complete"
echo "============================================"
echo ""
echo "Results directory: $RESULTS_DIR"
echo ""
echo "Files:"
echo "  phase_log.csv        - Timestamped phase transitions"
echo "  locust_stats.csv     - Aggregated request statistics"
echo "  locust_stats_history.csv - Time-series request data"
echo "  locust_failures.csv  - Failed requests detail"
echo "  locust_output.log    - Full Locust output"
echo ""
echo "Analysis tips:"
echo "  1. Compare p50/p95/p99 latency across phases using phase_log.csv timestamps"
echo "  2. Check error rates in locust_failures.csv during REPLICA_FAILURE phase"
echo "  3. Compare locust_stats_history.csv timeline latency before/after REDIS_FLUSH"
echo "  4. Check CloudWatch RDS metrics for replica query distribution:"
echo "     aws cloudwatch get-metric-statistics --namespace AWS/RDS \\"
echo "       --metric-name ReadIOPS --dimensions Name=DBInstanceIdentifier,Value=$REPLICA_ID \\"
echo "       --start-time <start> --end-time <end> --period 60 --statistics Average"
echo ""
