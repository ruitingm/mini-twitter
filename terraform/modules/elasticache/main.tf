# ElastiCache Redis cluster for timeline caching and rate limiting
resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.service_name}-cache-subnet"
  subnet_ids = var.subnet_ids
}

# Security group for Redis access from ECS services
resource "aws_security_group" "redis" {
  name_prefix = "${var.service_name}-redis-"
  vpc_id      = var.vpc_id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [var.ecs_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.service_name}-redis-sg"
  }
}

# Redis cluster - Small instance for cost efficiency
resource "aws_elasticache_cluster" "redis" {
  cluster_id           = var.service_name
  engine               = "redis"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
  port                 = 6379
  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.redis.id]

  tags = {
    Name = "${var.service_name}-redis"
  }
}