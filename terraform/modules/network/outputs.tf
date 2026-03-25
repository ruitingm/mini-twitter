output "security_group_id" {
  value = aws_security_group.this.id
}

output "public_subnet_ids" {
  value = data.aws_subnets.public.ids
}

output "private_subnet_ids" {
  value = data.aws_subnets.private.ids
}

# General-purpose subnet output:
# use private subnets if they exist, otherwise fall back to public subnets.
output "subnet_ids" {
  value = length(data.aws_subnets.private.ids) > 0 ? data.aws_subnets.private.ids : data.aws_subnets.public.ids
}

output "vpc_id" {
  value = data.aws_vpc.default.id
}

# ALB outputs - For service-to-service communication
output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = aws_lb.this.dns_name
}

output "alb_zone_id" {
  description = "Zone ID of the Application Load Balancer"
  value       = aws_lb.this.zone_id
}

# Target group ARNs - For ECS service registration
output "gateway_target_group_arn" {
  description = "Target group ARN for gateway service"
  value       = aws_lb_target_group.gateway.arn
}

output "user_target_group_arn" {
  description = "Target group ARN for user service"
  value       = aws_lb_target_group.user.arn
}

output "tweet_target_group_arn" {
  description = "Target group ARN for tweet service"
  value       = aws_lb_target_group.tweet.arn
}

output "timeline_target_group_arn" {
  description = "Target group ARN for timeline service"
  value       = aws_lb_target_group.timeline.arn
}