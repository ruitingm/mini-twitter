output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = module.network.alb_dns_name
}

output "ecs_cluster_name" {
  description = "Name of the created ECS cluster"
  value       = module.ecs_gateway.cluster_name
}

output "ecs_service_names" {
  description = "Names of the running ECS services"
  value = {
    gateway  = module.ecs_gateway.service_name
    user     = module.ecs_user.service_name
    tweet    = module.ecs_tweet.service_name
    timeline = module.ecs_timeline.service_name
  }
}