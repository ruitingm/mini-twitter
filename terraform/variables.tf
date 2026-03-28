# Region to deploy into
variable "aws_region" {
  type    = string
  default = "us-west-2"
}

# ECR & ECS settings - Updated for mini-twitter microservices
variable "ecr_repository_name" {
  type    = string
  default = "mini-twitter"
}

variable "service_name" {
  type    = string
  default = "mini-twitter"
}

variable "container_port" {
  type    = number
  default = 8080
}

variable "ecs_count" {
  type    = number
  default = 1
}

# How long to keep logs
variable "log_retention_days" {
  type    = number
  default = 7
}

# Database variables - Updated for PostgreSQL
variable "db_username" {
  description = "Username for RDS PostgreSQL"
  type        = string
  default     = "twitter"
}

variable "db_password" {
  description = "Password for RDS PostgreSQL"
  type        = string
  sensitive   = true
  default     = "twitter123"
}

# Experiment configuration - Variables for testing Redis vs PostgreSQL
variable "use_redis" {
  description = "Use Redis for caching (true) or direct PostgreSQL (false)"
  type        = bool
  default     = true
}

variable "consistency_mode" {
  description = "Consistency mode: eventual or strong"
  type        = string
  default     = "eventual" 
  validation {
    condition     = contains(["eventual", "strong"], var.consistency_mode)
    error_message = "Consistency mode must be either 'eventual' or 'strong'."
  }
}
