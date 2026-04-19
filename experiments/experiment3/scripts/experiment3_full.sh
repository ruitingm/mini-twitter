#!/usr/bin/env bash
# ============================================================================
# Experiment 3: Failure Resilience Tests (Full Version)
# ============================================================================
# Runs three INDEPENDENT test scenarios with high load to produce meaningful
# degradation data when components fail:
#
#   Scenario 1 - BASELINE:        All components healthy under stress load
#   Scenario 2 - REPLICA FAILURE: Reboot one read replica under stress load
#   Scenario 3 - REDIS FLUSH:     Flush entire Redis cache under stress load
#
# Each scenario is a separate Locust run with its own CSV output, ensuring
# clean data with no cross-contamination between tests.
#
# Prerequisites:
#   - AWS CLI configured
#   - Locust installed
#   - Test data seeded (./scripts/seed_test_data_large.sh)
#   - session-manager-plugin available (for Redis flush via ECS Exec)
#
# Usage (run from project root):
#   export BASE_URL="http://<alb-dns>"
#   export REDIS_HOST="<elasticache-endpoint>"
#   export PATH="$HOME/bin:$PATH"  # for session-manager-plugin
#   ./experiments/experiment3/scripts/experiment3_full.sh
# ============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
BASE_URL="${BASE_URL:?Set BASE_URL to ALB DNS}"
REDIS_HOST="${REDIS_HOST:?Set REDIS_HOST to ElastiCache endpoint}"
REDIS_PORT="${REDIS_PORT:-6379}"
REPLICA_ID="${REPLICA_ID:-mini-twitter-postgres-replica2}"

# Load parameters -- tuned to stress db.t3.micro instances
USERS="${USERS:-200}"
SPAWN_RATE="${SPAWN_RATE:-20}"
LOCUST_FILE="${LOCUST_FILE:-experiments/experiment3/locust/locustfile_stress.py}"

# Phase durations (seconds)
WARMUP="${WARMUP:-60}"            # let load stabilize before measuring
BASELINE_MEASURE="${BASELINE_MEASURE:-180}"  # 3 min steady-state measurement
PRE_FAILURE="${PRE_FAILURE:-120}"  # 2 min pre-failure baseline in failure runs
POST_FAILURE="${POST_FAILURE:-180}" # 3 min post-failure measurement
RECOVERY_WAIT="${RECOVERY_WAIT:-180}" # 3 min post-recovery measurement

# ECS Exec config (for Redis FLUSHALL)
ECS_CLUSTER="${ECS_CLUSTER:-mini-twitter-gateway-cluster}"
ECS_SERVICE="${ECS_SERVICE:-mini-twitter-gateway}"
ECS_CONTAINER="${ECS_CONTAINER:-mini-twitter-gateway-container}"

# Results directory
RUN_ID=$(date +%Y%m%d_%H%M%S)
RESULTS_BASE="experiments/experiment3/results/$RUN_ID"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log() {
    local scenario="$1" phase="$2" msg="$3"
    local ts
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    echo "[$ts] [$scenario/$phase] $msg"
    echo "$ts,$scenario,$phase,$msg" >> "$RESULTS_BASE/experiment_log.csv"
}

start_locust() {
    local scenario="$1"
    local duration="$2"
    local csv_dir="$RESULTS_BASE/$scenario"
    mkdir -p "$csv_dir"

    # Redirect all output to file (NOT tee) to avoid $() blocking on stdout
    locust -f "$LOCUST_FILE" \
        --headless \
        -u "$USERS" \
        -r "$SPAWN_RATE" \
        -t "${duration}s" \
        --host="$BASE_URL" \
        --csv="$csv_dir/locust" \
        --csv-full-history \
        --html="$csv_dir/locust_report.html" \
        --only-summary > "$csv_dir/locust_output.log" 2>&1 &

    LOCUST_PID=$!
    echo "$LOCUST_PID"
}

stop_locust() {
    local pid="$1"
    if kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
}

wait_for_warmup() {
    local scenario="$1"
    # Wait for ramp-up + warmup
    local ramp=$(( USERS / SPAWN_RATE + 5 ))
    log "$scenario" "WARMUP" "Ramp-up ${ramp}s + warm-up ${WARMUP}s"
    sleep "$ramp"
    sleep "$WARMUP"
    log "$scenario" "WARMUP" "Complete -- load stabilized"
}

flush_redis() {
    local scenario="$1"
    # Try direct redis-cli first, then ECS Exec with netcat
    if command -v redis-cli >/dev/null 2>&1; then
        redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" FLUSHALL 2>/dev/null && {
            log "$scenario" "FLUSH" "FLUSHALL via redis-cli"
            return 0
        }
    fi

    TASK_ARN=$(aws ecs list-tasks --cluster "$ECS_CLUSTER" \
        --service-name "$ECS_SERVICE" --desired-status RUNNING \
        --query 'taskArns[0]' --output text 2>/dev/null || echo "None")

    if [[ -n "$TASK_ARN" && "$TASK_ARN" != "None" ]]; then
        FLUSH_OUT=$(aws ecs execute-command \
            --cluster "$ECS_CLUSTER" --task "$TASK_ARN" \
            --container "$ECS_CONTAINER" \
            --command "/bin/sh -c 'echo FLUSHALL | nc $REDIS_HOST $REDIS_PORT'" \
            --interactive 2>&1)
        if echo "$FLUSH_OUT" | grep -q "+OK"; then
            log "$scenario" "FLUSH" "FLUSHALL via ECS Exec (+OK)"
            return 0
        fi
    fi

    log "$scenario" "FLUSH" "WARNING: Could not flush Redis"
    return 1
}

collect_cloudwatch() {
    local scenario="$1"
    local start_time="$2"
    local end_time="$3"
    local csv_dir="$RESULTS_BASE/$scenario"

    log "$scenario" "METRICS" "Collecting CloudWatch metrics..."

    # RDS primary CPU
    aws cloudwatch get-metric-statistics \
        --namespace AWS/RDS \
        --metric-name CPUUtilization \
        --dimensions Name=DBInstanceIdentifier,Value=mini-twitter-postgres-primary \
        --start-time "$start_time" --end-time "$end_time" \
        --period 60 --statistics Average Maximum \
        --output json > "$csv_dir/cw_rds_primary_cpu.json" 2>/dev/null || true

    # RDS replica1 CPU
    aws cloudwatch get-metric-statistics \
        --namespace AWS/RDS \
        --metric-name CPUUtilization \
        --dimensions Name=DBInstanceIdentifier,Value=mini-twitter-postgres-replica1 \
        --start-time "$start_time" --end-time "$end_time" \
        --period 60 --statistics Average Maximum \
        --output json > "$csv_dir/cw_rds_replica1_cpu.json" 2>/dev/null || true

    # RDS replica2 CPU
    aws cloudwatch get-metric-statistics \
        --namespace AWS/RDS \
        --metric-name CPUUtilization \
        --dimensions Name=DBInstanceIdentifier,Value="$REPLICA_ID" \
        --start-time "$start_time" --end-time "$end_time" \
        --period 60 --statistics Average Maximum \
        --output json > "$csv_dir/cw_rds_replica2_cpu.json" 2>/dev/null || true

    # RDS primary DatabaseConnections
    aws cloudwatch get-metric-statistics \
        --namespace AWS/RDS \
        --metric-name DatabaseConnections \
        --dimensions Name=DBInstanceIdentifier,Value=mini-twitter-postgres-primary \
        --start-time "$start_time" --end-time "$end_time" \
        --period 60 --statistics Average Maximum \
        --output json > "$csv_dir/cw_rds_primary_connections.json" 2>/dev/null || true

    # RDS ReadIOPS for replicas
    for replica in mini-twitter-postgres-replica1 "$REPLICA_ID"; do
        safe_name=$(echo "$replica" | tr '-' '_')
        aws cloudwatch get-metric-statistics \
            --namespace AWS/RDS \
            --metric-name ReadIOPS \
            --dimensions Name=DBInstanceIdentifier,Value="$replica" \
            --start-time "$start_time" --end-time "$end_time" \
            --period 60 --statistics Average \
            --output json > "$csv_dir/cw_rds_${safe_name}_read_iops.json" 2>/dev/null || true
    done

    log "$scenario" "METRICS" "CloudWatch metrics saved"
}

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------
echo "============================================================"
echo "  Experiment 3: Failure Resilience Tests (Full)"
echo "============================================================"
echo ""
echo "  ALB:         $BASE_URL"
echo "  Redis:       $REDIS_HOST"
echo "  Replica:     $REPLICA_ID"
echo "  Users:       $USERS (spawn rate: $SPAWN_RATE)"
echo "  Locust file: $LOCUST_FILE"
echo ""

command -v locust >/dev/null 2>&1 || { echo "ERROR: locust not found"; exit 1; }
command -v aws    >/dev/null 2>&1 || { echo "ERROR: aws not found"; exit 1; }

[[ -f "testing/test_users.json" ]] || { echo "ERROR: test_users.json missing"; exit 1; }

# Token check
SAMPLE_TOKEN=$(python3 -c "import json; print(json.load(open('testing/test_users.json'))[0]['token'])")
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $SAMPLE_TOKEN" "$BASE_URL/v1/timeline/home" 2>/dev/null || echo "000")
if [[ "$HTTP_CODE" == "401" ]]; then echo "ERROR: Tokens expired. Re-seed."; exit 1; fi
if [[ "$HTTP_CODE" == "000" ]]; then echo "ERROR: Cannot reach ALB."; exit 1; fi
echo "Pre-flight OK (HTTP $HTTP_CODE)"
echo ""

mkdir -p "$RESULTS_BASE"
echo "timestamp,scenario,phase,event" > "$RESULTS_BASE/experiment_log.csv"

# =========================================================================
# SCENARIO 1: BASELINE (all components healthy)
# =========================================================================
echo ""
echo "============================================================"
echo "  SCENARIO 1: BASELINE (all healthy)"
echo "============================================================"

SCENARIO="baseline"
# Add 120s buffer to ensure Locust outlives the script's sleep phases
S1_DURATION=$(( WARMUP + USERS/SPAWN_RATE + 5 + BASELINE_MEASURE + 120 ))

log "$SCENARIO" "START" "Starting baseline test: ${USERS} users for ${S1_DURATION}s total"
S1_START=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

PID=$(start_locust "$SCENARIO" "$S1_DURATION")
log "$SCENARIO" "LOCUST" "Started (PID: $PID)"

wait_for_warmup "$SCENARIO"
log "$SCENARIO" "MEASURE" "Measuring steady-state for ${BASELINE_MEASURE}s"
sleep "$BASELINE_MEASURE"
log "$SCENARIO" "MEASURE" "Complete"

stop_locust "$PID"
S1_END=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
log "$SCENARIO" "END" "Baseline scenario complete"

collect_cloudwatch "$SCENARIO" "$S1_START" "$S1_END"

# Brief cooldown between scenarios
echo "Cooling down 30s between scenarios..."
sleep 30

# =========================================================================
# SCENARIO 2: REPLICA FAILURE
# =========================================================================
echo ""
echo "============================================================"
echo "  SCENARIO 2: REPLICA FAILURE"
echo "============================================================"

SCENARIO="replica_failure"
S2_DURATION=$(( WARMUP + USERS/SPAWN_RATE + 5 + PRE_FAILURE + POST_FAILURE + RECOVERY_WAIT + 120 ))

log "$SCENARIO" "START" "Starting replica failure test: ${USERS} users for ${S2_DURATION}s total"
S2_START=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

PID=$(start_locust "$SCENARIO" "$S2_DURATION")
log "$SCENARIO" "LOCUST" "Started (PID: $PID)"

wait_for_warmup "$SCENARIO"

# Pre-failure measurement
log "$SCENARIO" "PRE_FAILURE" "Measuring pre-failure baseline for ${PRE_FAILURE}s"
sleep "$PRE_FAILURE"
log "$SCENARIO" "PRE_FAILURE" "Complete"

# Inject failure: reboot replica
log "$SCENARIO" "INJECT" "Rebooting RDS replica $REPLICA_ID"
FAILURE_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
aws rds reboot-db-instance --db-instance-identifier "$REPLICA_ID" > /dev/null 2>&1 || {
    log "$SCENARIO" "INJECT" "WARNING: Reboot command failed"
}
log "$SCENARIO" "INJECT" "Reboot command sent at $FAILURE_TS"

# Measure during failure
log "$SCENARIO" "DURING_FAILURE" "Measuring during failure for ${POST_FAILURE}s"
sleep "$POST_FAILURE"
log "$SCENARIO" "DURING_FAILURE" "Complete"

# Wait for recovery
log "$SCENARIO" "RECOVERY" "Waiting for replica to recover..."
RECOVERY_START=$(date +%s)
aws rds wait db-instance-available --db-instance-identifier "$REPLICA_ID" 2>/dev/null || true
RECOVERY_ELAPSED=$(( $(date +%s) - RECOVERY_START ))
log "$SCENARIO" "RECOVERY" "Replica available after ${RECOVERY_ELAPSED}s"

# Measure post-recovery
RECOVERY_REMAINING=$(( RECOVERY_WAIT - RECOVERY_ELAPSED ))
if [[ $RECOVERY_REMAINING -gt 0 ]]; then
    log "$SCENARIO" "POST_RECOVERY" "Measuring post-recovery for ${RECOVERY_REMAINING}s"
    sleep "$RECOVERY_REMAINING"
fi
log "$SCENARIO" "POST_RECOVERY" "Complete"

stop_locust "$PID"
S2_END=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
log "$SCENARIO" "END" "Replica failure scenario complete"

collect_cloudwatch "$SCENARIO" "$S2_START" "$S2_END"

# Brief cooldown
echo "Cooling down 30s between scenarios..."
sleep 30

# =========================================================================
# SCENARIO 3: REDIS CACHE FLUSH
# =========================================================================
echo ""
echo "============================================================"
echo "  SCENARIO 3: REDIS CACHE FLUSH"
echo "============================================================"

SCENARIO="redis_flush"
S3_DURATION=$(( WARMUP + USERS/SPAWN_RATE + 5 + PRE_FAILURE + POST_FAILURE + 120 ))

log "$SCENARIO" "START" "Starting Redis flush test: ${USERS} users for ${S3_DURATION}s total"
S3_START=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

PID=$(start_locust "$SCENARIO" "$S3_DURATION")
log "$SCENARIO" "LOCUST" "Started (PID: $PID)"

wait_for_warmup "$SCENARIO"

# Pre-flush measurement (let Redis cache warm up)
log "$SCENARIO" "PRE_FLUSH" "Measuring with warm cache for ${PRE_FAILURE}s"
sleep "$PRE_FAILURE"
log "$SCENARIO" "PRE_FLUSH" "Complete"

# Inject failure: flush Redis
FLUSH_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
log "$SCENARIO" "INJECT" "Flushing Redis at $FLUSH_TS"
flush_redis "$SCENARIO" || true

# Measure post-flush
log "$SCENARIO" "POST_FLUSH" "Measuring post-flush for ${POST_FAILURE}s"
sleep "$POST_FAILURE"
log "$SCENARIO" "POST_FLUSH" "Complete"

stop_locust "$PID"
S3_END=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
log "$SCENARIO" "END" "Redis flush scenario complete"

collect_cloudwatch "$SCENARIO" "$S3_START" "$S3_END"

# =========================================================================
# Generate analysis
# =========================================================================
echo ""
echo "============================================================"
echo "  Running Analysis"
echo "============================================================"

ANALYZE_SCRIPT="experiments/experiment3/scripts/experiment3_analyze.py"
if [[ -f "$ANALYZE_SCRIPT" ]]; then
    python3 "$ANALYZE_SCRIPT" "$RESULTS_BASE" 2>&1 | tee "$RESULTS_BASE/analysis_report.txt"
fi

echo ""
echo "============================================================"
echo "  Generating Charts"
echo "============================================================"

VISUALIZE_SCRIPT="experiments/experiment3/scripts/experiment3_visualize.py"
if [[ -f "$VISUALIZE_SCRIPT" ]]; then
    python3 "$VISUALIZE_SCRIPT" "$RESULTS_BASE" 2>&1
fi

echo ""
echo "============================================================"
echo "  Experiment 3 Complete"
echo "============================================================"
echo ""
echo "Results: $RESULTS_BASE/"
echo ""
echo "  baseline/           -- Scenario 1: all healthy"
echo "  replica_failure/    -- Scenario 2: replica rebooted mid-test"
echo "  redis_flush/        -- Scenario 3: Redis flushed mid-test"
echo "  experiment_log.csv  -- Timestamped event log"
echo "  analysis_report.txt -- Comparison analysis"
echo ""
