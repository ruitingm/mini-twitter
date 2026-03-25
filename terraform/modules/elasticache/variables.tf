# ElastiCache module input variables
variable "service_name" {
  description = "Name of the service for resource naming"
  type        = string
}

variable "subnet_ids" {
  description = "List of subnet IDs for ElastiCache subnet group"
  type        = list(string)
}

variable "vpc_id" {
  description = "VPC ID for security group"
  type        = string
}

variable "ecs_security_group_id" {
  description = "Security group ID of ECS services for Redis access"
  type        = string
}