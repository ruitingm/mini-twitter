variable "service_name" {
  type        = string
  description = "Base name for ECS resources"
}

variable "image" {
  type        = string
  description = "ECR image URI (with tag)"
}

variable "container_port" {
  type        = number
  description = "Port your app listens on"
}

variable "subnet_ids" {
  type        = list(string)
  description = "Subnets for FARGATE tasks"
}

variable "security_group_ids" {
  type        = list(string)
  description = "SGs for FARGATE tasks"
}

variable "execution_role_arn" {
  type        = string
  description = "ECS Task Execution Role ARN"
}

variable "task_role_arn" {
  type        = string
  description = "IAM Role ARN for app permissions"
}

variable "log_group_name" {
  type        = string
  description = "CloudWatch log group name"
}

variable "ecs_count" {
  type        = number
  default     = 1
  description = "Desired Fargate task count"
}

variable "region" {
  type        = string
  description = "AWS region (for awslogs driver)"
}

variable "cpu" {
  type        = string
  default     = "256"
  description = "vCPU units"
}

variable "memory" {
  type        = string
  default     = "512"
  description = "Memory (MiB)"
}

# Service type - Determines which environment variables to set
variable "service_type" {
  type        = string
  description = "Type of service: gateway, user, tweet, or timeline"
}

# Database variables - Optional based on service type
variable "db_host" {
  type        = string
  default     = ""
  description = "Database host"
}

variable "db_port" {
  type        = string
  default     = ""
  description = "Database port"
}

variable "db_name" {
  type        = string
  default     = ""
  description = "Database name"
}

variable "db_username" {
  type        = string
  default     = ""
  description = "Database username"
}

variable "db_password" {
  type        = string
  default     = ""
  description = "Database password"
}

variable "db_replica_urls" {
  type        = string
  default     = ""
  description = "Comma-separated replica URLs"
}

# Redis variables - For caching and rate limiting
variable "redis_addr" {
  type        = string
  default     = ""
  description = "Redis endpoint address"
}

# Inter-service communication URLs
variable "user_service_url" {
  type        = string
  default     = ""
  description = "User service internal URL"
}

variable "tweet_service_url" {
  type        = string
  default     = ""
  description = "Tweet service internal URL"
}

variable "timeline_service_url" {
  type        = string
  default     = ""
  description = "Timeline service internal URL"
}

# Experiment configuration variables
variable "fanout_strategy" {
  type        = string
  default     = "write"
  description = "Fan-out strategy: write or read"
}

variable "consistency_mode" {
  type        = string
  default     = "eventual"
  description = "Consistency mode: eventual or strong"
}

variable "jwt_secret" {
  type        = string
  default     = "supersecretjwtkey"
  description = "JWT signing secret"
}

# Load balancer target group ARN for service registration
variable "target_group_arn" {
  type        = string
  default     = ""
  description = "ALB target group ARN for this service"
}
