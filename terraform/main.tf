# Wire together focused modules for mini-twitter microservices: network, ecr, logging, ecs, rds, elasticache

module "network" {
  source         = "./modules/network"
  service_name   = var.service_name
  container_port = var.container_port
}

# ECR repositories - Create separate repos for each microservice
module "ecr_gateway" {
  source          = "./modules/ecr"
  repository_name = "${var.ecr_repository_name}-gateway"
}

module "ecr_user" {
  source          = "./modules/ecr"
  repository_name = "${var.ecr_repository_name}-user"
}

module "ecr_tweet" {
  source          = "./modules/ecr"
  repository_name = "${var.ecr_repository_name}-tweet"
}

module "ecr_timeline" {
  source          = "./modules/ecr"
  repository_name = "${var.ecr_repository_name}-timeline"
}

module "logging" {
  source            = "./modules/logging"
  service_name      = var.service_name
  retention_in_days = var.log_retention_days
}

# IAM role for ECS tasks (execution + task role)
resource "aws_iam_role" "ecs_role" {
  name = "${var.service_name}-ecs-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_execution_policy" {
  role       = aws_iam_role.ecs_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# ECS services - Create 4 separate microservices
module "ecs_gateway" {
  source             = "./modules/ecs"
  service_name       = "${var.service_name}-gateway"
  image              = "${module.ecr_gateway.repository_url}:latest"
  container_port     = 8080
  subnet_ids         = module.network.public_subnet_ids
  security_group_ids = [module.network.security_group_id]
  execution_role_arn = aws_iam_role.ecs_role.arn
  task_role_arn      = aws_iam_role.ecs_role.arn
  log_group_name     = module.logging.log_group_name
  ecs_count          = var.ecs_count
  region             = var.aws_region

  # Gateway-specific environment variables
  service_type         = "gateway"
  redis_addr           = module.elasticache.endpoint
  user_service_url     = "http://${module.network.alb_dns_name}/internal/user"
  tweet_service_url    = "http://${module.network.alb_dns_name}/internal/tweet"
  timeline_service_url = "http://${module.network.alb_dns_name}/internal/timeline"
  use_redis            = var.use_redis
  consistency_mode     = var.consistency_mode
  jwt_secret           = "supersecretjwtkey"
  rate_limit_rpm       = var.rate_limit_rpm
  target_group_arn     = module.network.gateway_target_group_arn

  # Auto-scaling
  enable_autoscaling       = var.enable_autoscaling
  autoscaling_min_capacity = var.autoscaling_min_capacity
  autoscaling_max_capacity = var.autoscaling_max_capacity
  autoscaling_cpu_target   = var.autoscaling_cpu_target
}

module "ecs_user" {
  source             = "./modules/ecs"
  service_name       = "${var.service_name}-user"
  image              = "${module.ecr_user.repository_url}:latest"
  container_port     = 8081
  subnet_ids         = module.network.public_subnet_ids
  security_group_ids = [module.network.security_group_id]
  execution_role_arn = aws_iam_role.ecs_role.arn
  task_role_arn      = aws_iam_role.ecs_role.arn
  log_group_name     = module.logging.log_group_name
  ecs_count          = var.ecs_count
  region             = var.aws_region

  # User service environment variables
  service_type     = "user"
  db_host          = module.rds.endpoint
  db_port          = tostring(module.rds.port)
  db_name          = module.rds.db_name
  db_username      = var.db_username
  db_password      = var.db_password
  db_replica_urls  = join(",", module.rds.replica_connection_strings)
  redis_addr       = module.elasticache.endpoint
  use_redis        = var.use_redis
  jwt_secret       = "supersecretjwtkey"
  rate_limit_rpm   = var.rate_limit_rpm
  target_group_arn = module.network.user_target_group_arn

  # Auto-scaling
  enable_autoscaling       = var.enable_autoscaling
  autoscaling_min_capacity = var.autoscaling_min_capacity
  autoscaling_max_capacity = var.autoscaling_max_capacity
  autoscaling_cpu_target   = var.autoscaling_cpu_target
}

module "ecs_tweet" {
  source             = "./modules/ecs"
  service_name       = "${var.service_name}-tweet"
  image              = "${module.ecr_tweet.repository_url}:latest"
  container_port     = 8082
  subnet_ids         = module.network.public_subnet_ids
  security_group_ids = [module.network.security_group_id]
  execution_role_arn = aws_iam_role.ecs_role.arn
  task_role_arn      = aws_iam_role.ecs_role.arn
  log_group_name     = module.logging.log_group_name
  ecs_count          = var.ecs_count
  region             = var.aws_region

  # Tweet service environment variables
  service_type     = "tweet"
  db_host          = module.rds.endpoint
  db_port          = tostring(module.rds.port)
  db_name          = module.rds.db_name
  db_username      = var.db_username
  db_password      = var.db_password
  db_replica_urls  = join(",", module.rds.replica_connection_strings)
  redis_addr       = module.elasticache.endpoint
  user_service_url = "http://${module.network.alb_dns_name}/internal/user"
  use_redis        = var.use_redis
  consistency_mode = var.consistency_mode
  jwt_secret       = "supersecretjwtkey"
  rate_limit_rpm   = var.rate_limit_rpm
  target_group_arn = module.network.tweet_target_group_arn

  # Auto-scaling
  enable_autoscaling       = var.enable_autoscaling
  autoscaling_min_capacity = var.autoscaling_min_capacity
  autoscaling_max_capacity = var.autoscaling_max_capacity
  autoscaling_cpu_target   = var.autoscaling_cpu_target
}

module "ecs_timeline" {
  source             = "./modules/ecs"
  service_name       = "${var.service_name}-timeline"
  image              = "${module.ecr_timeline.repository_url}:latest"
  container_port     = 8083
  subnet_ids         = module.network.public_subnet_ids
  security_group_ids = [module.network.security_group_id]
  execution_role_arn = aws_iam_role.ecs_role.arn
  task_role_arn      = aws_iam_role.ecs_role.arn
  log_group_name     = module.logging.log_group_name
  ecs_count          = var.ecs_count
  region             = var.aws_region

  # Timeline service environment variables
  service_type      = "timeline"
  db_host           = module.rds.endpoint
  db_port           = tostring(module.rds.port)
  db_name           = module.rds.db_name
  db_username       = var.db_username
  db_password       = var.db_password
  db_replica_urls   = join(",", module.rds.replica_connection_strings)
  redis_addr        = module.elasticache.endpoint
  use_redis         = var.use_redis
  tweet_service_url = "http://${module.network.alb_dns_name}/internal/tweet"
  jwt_secret        = "supersecretjwtkey"
  rate_limit_rpm    = var.rate_limit_rpm
  target_group_arn  = module.network.timeline_target_group_arn

  # Auto-scaling
  enable_autoscaling       = var.enable_autoscaling
  autoscaling_min_capacity = var.autoscaling_min_capacity
  autoscaling_max_capacity = var.autoscaling_max_capacity
  autoscaling_cpu_target   = var.autoscaling_cpu_target
}


# Docker images - Build and push 4 separate service images
resource "docker_image" "gateway" {
  name = "${module.ecr_gateway.repository_url}:latest"
  build {
    context    = ".."
    dockerfile = "Dockerfile"
    build_args = {
      SERVICE : "gateway"
    }
  }
  triggers = {
    rebuild = timestamp()
  }
}

resource "docker_image" "user" {
  name = "${module.ecr_user.repository_url}:latest"
  build {
    context    = ".."
    dockerfile = "Dockerfile"
    build_args = {
      SERVICE : "user"
    }
  }
  triggers = {
    rebuild = timestamp()
  }
}

resource "docker_image" "tweet" {
  name = "${module.ecr_tweet.repository_url}:latest"
  build {
    context    = ".."
    dockerfile = "Dockerfile"
    build_args = {
      SERVICE : "tweet"
    }
  }
  triggers = {
    rebuild = timestamp()
  }
}

resource "docker_image" "timeline" {
  name = "${module.ecr_timeline.repository_url}:latest"
  build {
    context    = ".."
    dockerfile = "Dockerfile"
    build_args = {
      SERVICE : "timeline"
    }
  }
  triggers = {
    rebuild = timestamp()
  }
}

# Docker registry images - Push all images to ECR
resource "docker_registry_image" "gateway" {
  name = docker_image.gateway.name
}

resource "docker_registry_image" "user" {
  name = docker_image.user.name
}

resource "docker_registry_image" "tweet" {
  name = docker_image.tweet.name
}

resource "docker_registry_image" "timeline" {
  name = docker_image.timeline.name
}

# RDS PostgreSQL - Updated from MySQL with read replicas
module "rds" {
  source = "./modules/rds"

  service_name = var.service_name
  subnet_ids   = module.network.subnet_ids
  vpc_id       = module.network.vpc_id

  # Allow ECS services to connect
  ecs_security_group_id = module.network.security_group_id

  db_name     = "twitter"
  db_username = var.db_username
  db_password = var.db_password
}

# ElastiCache Redis - New module for caching layer
module "elasticache" {
  source = "./modules/elasticache"

  service_name = var.service_name
  subnet_ids   = module.network.subnet_ids
  vpc_id       = module.network.vpc_id

  # Allow ECS services to connect to Redis
  ecs_security_group_id = module.network.security_group_id
}
