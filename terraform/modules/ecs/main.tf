# ECS Cluster
resource "aws_ecs_cluster" "this" {
  name = "${var.service_name}-cluster"
}

# Task Definition
resource "aws_ecs_task_definition" "this" {
  family                   = "${var.service_name}-task"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.cpu
  memory                   = var.memory

  execution_role_arn = var.execution_role_arn
  task_role_arn      = var.task_role_arn

  container_definitions = jsonencode([{
    name      = "${var.service_name}-container"
    image     = var.image
    essential = true

    portMappings = [{
      containerPort = var.container_port
      hostPort      = var.container_port
    }]

    # Environment variables - Service-specific configuration
    environment = concat(
      [
        {
          name  = "SERVICE_NAME"
          value = var.service_name
        },
        {
          name  = "PORT"
          value = tostring(var.container_port)
        }
      ],
      # Database variables - Only for services that need DB access
      var.db_host != "" ? [
        {
          name  = "POSTGRES_PRIMARY_URL"
          value = "postgres://${var.db_username}:${var.db_password}@${var.db_host}:${var.db_port}/${var.db_name}"
        }
      ] : [],
      var.db_replica_urls != "" ? [
        {
          name  = "POSTGRES_REPLICA_URLS"
          value = var.db_replica_urls
        }
      ] : [],
      # Redis variables - For caching services
      var.redis_addr != "" ? [
        {
          name  = "REDIS_ADDR"
          value = var.redis_addr
        }
      ] : [],
      # Inter-service URLs
      var.user_service_url != "" ? [
        {
          name  = "USER_SERVICE_URL"
          value = var.user_service_url
        }
      ] : [],
      var.tweet_service_url != "" ? [
        {
          name  = "TWEET_SERVICE_URL"
          value = var.tweet_service_url
        }
      ] : [],
      var.timeline_service_url != "" ? [
        {
          name  = "TIMELINE_SERVICE_URL"
          value = var.timeline_service_url
        }
      ] : [],
      # Experiment configuration
      [
        {
          name  = "USE_REDIS"
          value = tostring(var.use_redis)
        },
        {
          name  = "CONSISTENCY_MODE"
          value = var.consistency_mode
        },
        {
          name  = "JWT_SECRET"
          value = var.jwt_secret
        },
        {
          name  = "JWT_EXPIRY"
          value = "24h"
        },
        {
          name  = "RATE_LIMIT_RPM"
          value = tostring(var.rate_limit_rpm)
        }
      ]
    )

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = var.log_group_name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "ecs"
      }
    }
  }])
}

# ECS Service - Updated with ALB target group registration
resource "aws_ecs_service" "this" {
  name            = var.service_name
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.this.arn
  desired_count   = var.ecs_count
  launch_type     = "FARGATE"

  # Load balancer configuration - Register with ALB target group
  dynamic "load_balancer" {
    for_each = var.target_group_arn != "" ? [1] : []
    content {
      target_group_arn = var.target_group_arn
      container_name   = "${var.service_name}-container"
      container_port   = var.container_port
    }
  }

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = var.security_group_ids
    assign_public_ip = true
  }

  # Health check grace period for ALB registration
  health_check_grace_period_seconds = var.target_group_arn != "" ? 30 : null

  # Ensure target group exists before creating service
  depends_on = [var.target_group_arn]

  # When auto-scaling is enabled, prevent Terraform from fighting the auto-scaler
  lifecycle {
    ignore_changes = [desired_count]
  }
}

# ---------------------------------------------------------------------------
# ECS Auto Scaling (conditional on enable_autoscaling)
# ---------------------------------------------------------------------------

# Register the ECS service as a scalable target
resource "aws_appautoscaling_target" "ecs" {
  count              = var.enable_autoscaling ? 1 : 0
  max_capacity       = var.autoscaling_max_capacity
  min_capacity       = var.autoscaling_min_capacity
  resource_id        = "service/${aws_ecs_cluster.this.name}/${aws_ecs_service.this.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
}

# CPU-based target tracking policy
resource "aws_appautoscaling_policy" "cpu" {
  count              = var.enable_autoscaling ? 1 : 0
  name               = "${var.service_name}-cpu-scaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs[0].resource_id
  scalable_dimension = aws_appautoscaling_target.ecs[0].scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs[0].service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = var.autoscaling_cpu_target
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}
