## Architecture Overview
The codebase implements a microservices-based Twitter clone with 4 containerized Go services as specified in the proposal:

### 1. API Gateway 
```cmd/gateway/main.go, internal/gateway/```
- **Purpose**: Routes requests, enforces rate limiting, aggregates responses
- **Key Components**:
  - Rate limiter backed by Redis ```internal/gateway/middleware.go```
  - JWT passthrough middleware ```gateway.JWTPassthrough()```
  - Reverse proxy handler routing to internal services
- **Configuration**: Runs on port 8080, configured via environment variables

### 2. User Service
```cmd/user/main.go, internal/user/```
- **Purpose**: Manages authentication, profiles, and follow/unfollow relationships
- **Repository Layer** ```internal/user/repository.go```: Raw SQL queries for users and follows
- **Service Layer** ```internal/user/service.go```: Business logic for registration, login, follow/unfollow
- **Handler Layer** ```internal/user/handler.go```: HTTP request/response translation
- **Database**: Uses primary for writes, read replicas for queries

### 3. Tweet Service
```cmd/tweet/main.go, internal/tweet/```
- **Purpose**: Handles tweet CRUD, likes, and fan-out operations
- **Key Features**:
  - **Configurable fanout strategy** ```internal/tweet/service.go```:
    - "write": Push tweet IDs to follower timelines at creation time
    - "read": Timeline service fetches on demand
  - **Configurable consistency mode** for likes ```internal/tweet/service.go```:
    - "strong": Synchronous PostgreSQL updates
    - "eventual": Redis counter with background aggregation
  - **Fan-out Worker** ```internal/tweet/fanout.go```: Pushes tweets to follower timelines
  - **Like Aggregator** ```internal/tweet/likeaggregator.go```: Batches like operations

### 4. Timeline Service
```cmd/timeline/main.go, internal/timeline/```
- **Purpose**: Serves materialized feeds from Redis with PostgreSQL fallback
- **Service Implementation** ```internal/timeline/service.go```:
  - GetHomeTimeline: Redis-first with DB fallback
  - GetUserTimeline: Direct DB queries
  - Tweet enrichment via batch API calls to tweet service
- **Cache Strategy**: 1000-tweet cache per user, 24-hour TTL


## Data Layer Implementation
### PostgreSQL Schema
- **users table** ```pkg/db/migrations/001_users.sql```: Core user data with follower/following counts
- **tweets table** ```pkg/db/migrations/002_tweets.sql```: Tweet content with like_count column
- **follows table** ```pkg/db/migrations/003_follows.sql```: Many-to-many follower relationships
- **likes table** ```pkg/db/migrations/004_likes.sql```: User-tweet like relationships
- **like_count_pending table** ```pkg/db/migrations/004_likes.sql```: Durability layer for eventual consistency

### Redis Cache Strategy
- **Timeline lists**: timeline:{user_id} - Ordered tweet IDs
- **Like counters**: like_count:{tweet_id} - Real-time counts
- **Rate limiting**: Per-IP counters for gateway throttling

### Database Replication
- Primary PostgreSQL instance with WAL streaming
- Two read replicas (postgres-replica1, postgres-replica2)
- Automatic failover handling in ```pkg/db/postgres.go```


## Experiment-Ready Features
### 1. Redis vs PostgreSQL Configuration
- **Environment variable**: USE_REDIS
- Easily switchable between Redis caching and direct PostgreSQL queries

### 2. Consistency Mode Configuration
- **Environment variable**: CONSISTENCY_MODE
- Like aggregator runs on configurable interval

### 3. Failure Resilience Features
- **Read replica failover**: Built into pkg/db/postgres.go
- **Redis fallback**: Timeline service falls back to PostgreSQL ```internal/timeline/service.go```
- **Circuit breaker pattern**: Rate limiter sheds load under pressure


## Monitoring & Observability
### Structured Logging ```pkg/logger/logger.go```
- Zerolog with service tagging
- Request ID tracking via middleware
- Error context preservation


## Other Components
### 1. Terraform Infrastructure
- AWS ECS task definitions for 4 microservices
- Application Load Balancer with path-based routing
- VPC/networking with security groups
- RDS PostgreSQL with 2 read replicas
- ElastiCache Redis cluster
- ECR repositories for Docker images
- Configurable experiment variables (fanout_strategy, consistency_mode)

### 2. Load Testing Scripts
- Seed datasets (small vs. large)
- Includes user registration, tweet creation, user relationship creation


## Key Implementation Highlights
### Fan-out-on-Write Implementation ```internal/tweet/fanout.go```
- Paginated follower fetching (500 at a time)
- Redis pipeline for batch updates
- Non-blocking job queue with load shedding

### Eventual Consistency Implementation ```internal/tweet/likeaggregator.go```
- Redis GETDEL for atomic delta retrieval
- PostgreSQL fallback for durability
- Batch updates to reduce write load

### Rate Limiting ```internal/gateway/middleware.go```
- Per-IP tracking in Redis
- Configurable requests per minute
- HTTP 429 responses when exceeded