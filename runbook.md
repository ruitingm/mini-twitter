# Deployment & Test Runbook

## Overview

This runbook describes the full workflow to:

* Deploy infrastructure (Terraform)
* Initialize database schema
* Verify end-to-end functionality
* Seed test data
* Run load testing (Locust)

---

## 0. Prerequisites

Ensure the following are ready:

* AWS credentials configured
* Docker running
* Terraform installed
* PostgreSQL client (`psql`) installed
* Python virtual environment (`.venv`) ready

---

## 1. Deploy Infrastructure (Terraform)

```bash
cd ~/Desktop/mini-twitter/terraform
terraform apply
```

After success, note the output:

```bash
alb_dns_name = "mini-twitter-alb-xxxxx.us-west-2.elb.amazonaws.com"
```

---

## 2. Initialize Database Schema

```bash
cd ~/Desktop/mini-twitter/pkg/db/migrations
export PGPASSWORD='twitter123'

export RDSHOST="<your-rds-endpoint>"
psql "host=$RDSHOST port=5432 dbname=twitter user=twitter sslmode=require"
```

Inside psql:

```sql
\i 001_users.sql
\i 002_tweets.sql
\i 003_follows.sql
\i 004_likes.sql
\dt
\q
```

Expected tables:

* users
* tweets
* follows
* likes
* like_count_pending

⚠️ Notes:

* Do NOT run `000_init_replication.sql`
* Must run from the `migrations` directory

---

## 3. Set Service Base URL

```bash
cd ~/Desktop/mini-twitter
export BASE_URL="http://<your-alb-dns>"
```

Example:

```bash
export BASE_URL="http://mini-twitter-alb-1519402236.us-west-2.elb.amazonaws.com"
```

---

## 4. Run End-to-End Test

```bash
./scripts/e2e_home_timeline.sh
```

This verifies:

* User registration
* Login
* Follow relationships
* Tweet creation
* Home timeline correctness

Expected result:

* Alice sees Bob’s tweet in timeline

---

## 5. Seed Test Data

Choose one option based on your testing needs:

### Option A: Small Dataset (Basic Testing)

```bash
./scripts/seed_test_data.sh
```

**Dataset size:**
* ~20 users
* ~100 tweets  
* ~100 follows
* **Time:** ~2 minutes

**Use for:** Basic functionality testing, quick verification

### Option B: Large Dataset (Performance Experiments)

```bash
./scripts/seed_test_data_large.sh
```

**Dataset size:**
* 300 users (with 1 celebrity user having ~150 followers)
* 15,000 tweets
* 4,500+ follows
* **Time:** ~5-8 minutes (parallelized)

**Use for:** stress testing, consistency testing, experiments

**Output file:** `testing/test_users.json`

---

## 6. Verify Database State

```bash
export PGPASSWORD='twitter123'

psql "host=$RDSHOST port=5432 dbname=twitter user=twitter sslmode=require"
```

Run:

```sql
SELECT COUNT(*) FROM users;
SELECT COUNT(*) FROM tweets;
SELECT COUNT(*) FROM follows;
\q
```

**Expected counts:**

### Small dataset:
* users ≈ 20+
* tweets ≈ 100+
* follows ≈ 100+

### Large dataset:
* users ≈ 300+
* tweets ≈ 15,000+
* follows ≈ 4,500+

---

## 7. Run Load Testing (Locust)

Activate environment:

```bash
cd ~/Desktop/mini-twitter
source .venv/bin/activate
```

Start Locust:

```bash
locust -f locustfile.py --host=$BASE_URL
```

Open browser:

```
http://0.0.0.0:8089
```

---

## 8. Load Test Guidelines

Recommended scenarios:

| Users         | Result                      |
| ------------- | --------------------------- |
| 10            | Stable                      |
| 50            | Stable                      |
| 100           | Usually stable              |
| 100+ high RPS | May hit 429 (rate limiting) |

---

## 9. Common Pitfalls

### Database

* Running SQL from wrong directory → file not found
* Running replication script → permission error
* SSL cert issues → use `sslmode=require`

### Terraform / Docker

* Docker not running → image build fails
* Disk full → terraform state backup error

### BASE_URL

* Not exported → defaults to localhost
* Wrong ALB → DNS resolve failure

### Scripts

* Running scripts from wrong directory
* Missing execute permission

---

## 10. Quick Re-run Checklist

```bash
# 1. Deploy
cd terraform && terraform apply

# 2. DB init
cd ../pkg/db/migrations
psql ...
\i 001_users.sql
\i 002_tweets.sql
\i 003_follows.sql
\i 004_likes.sql

# 3. Back to root
cd ~/Desktop/mini-twitter
export BASE_URL=...

# 4. Verify
./scripts/e2e_home_timeline.sh

# 5. Seed
./scripts/seed_test_data.sh

# 6. Load test
source .venv/bin/activate
locust -f locustfile.py --host=$BASE_URL
```

---

## Summary

Workflow:

```
Terraform → DB Init → E2E Test → Seed Data → Verify DB → Load Test
```

If all steps pass:

* System is fully deployed
* Data pipeline is working
* Service is ready for performance testing

---
