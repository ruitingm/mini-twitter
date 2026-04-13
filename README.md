## Distributed Social Media Backend
### Overview
This project builds a distributed microservices-based backend designed to model the challenges of modern social media systems, particularly under read-heavy workloads and bursty traffic patterns. 

The system consists of core services (User, Tweet, Timeline, and API Gateway) deployed on AWS, using PostgreSQL for persistent storage and Redis for caching materialized timelines. 

### System Design & Goals
By implementing a fan-out-on-write strategy and simulating realistic workloads, the project explores how architectural decisions - such as caching strategies and consistency models - affect latency, throughput, and scalability in high-concurrency environments.

We built this system to systematically evaluate key distributed systems tradeoffs, including the impact of caching, strong vs. eventual consistency, and failure resilience. Through controlled experiments and load testing, the project demonstrates how different design choices influence system behavior and user experience, particularly in large-scale, read-heavy applications.


### Documentation
Check out [codebase_walkthrough](codebase_walkthrough.md) for architecture overview and implementation details.

Check out the [runbook](runbook.md) for deployment and testing instructions.