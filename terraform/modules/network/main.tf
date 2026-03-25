# Fetch the default VPC
data "aws_vpc" "default" {
  default = true
}

# Public subnets
data "aws_subnets" "public" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }

  filter {
    name   = "map-public-ip-on-launch"
    values = ["true"]
  }
}

# Private subnets
data "aws_subnets" "private" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }

  filter {
    name   = "map-public-ip-on-launch"
    values = ["false"]
  }
}

# Create a security group to allow HTTP to your container port
resource "aws_security_group" "this" {
  name        = "${var.service_name}-sg"
  description = "Allow inbound on ${var.container_port}"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = var.container_port
    to_port     = var.container_port
    protocol    = "tcp"
    cidr_blocks = var.cidr_blocks
    description = "Allow HTTP traffic"
  }

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow HTTP from ALB"
  }

  ingress {
    from_port   = 8080
    to_port     = 8083
    protocol    = "tcp"
    self        = true
    description = "Allow service-to-service communication"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }
}

# Application Load Balancer - Routes traffic to microservices
resource "aws_lb" "this" {
  name               = "${var.service_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = data.aws_subnets.public.ids

  enable_deletion_protection = false

  tags = {
    Name = "${var.service_name}-alb"
  }
}

# ALB Security Group - Allow HTTP traffic from internet
resource "aws_security_group" "alb" {
  name        = "${var.service_name}-alb-sg"
  description = "Security group for ALB"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow HTTP from internet"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = {
    Name = "${var.service_name}-alb-sg"
  }
}

# Target Groups - One for each microservice
resource "aws_lb_target_group" "gateway" {
  name     = "${var.service_name}-gateway-tg"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    enabled             = true
    healthy_threshold   = 2
    unhealthy_threshold = 2
    timeout             = 5
    interval            = 30
    path                = "/"
    matcher             = "200"
  }

  tags = {
    Name = "${var.service_name}-gateway-tg"
  }
}

resource "aws_lb_target_group" "user" {
  name     = "${var.service_name}-user-tg"
  port     = 8081
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    enabled             = true
    healthy_threshold   = 2
    unhealthy_threshold = 2
    timeout             = 5
    interval            = 30
    path                = "/v1/users/health"
    matcher             = "200,404"
  }

  tags = {
    Name = "${var.service_name}-user-tg"
  }
}

resource "aws_lb_target_group" "tweet" {
  name     = "${var.service_name}-tweet-tg"
  port     = 8082
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    enabled             = true
    healthy_threshold   = 2
    unhealthy_threshold = 2
    timeout             = 5
    interval            = 30
    path                = "/health"
    matcher             = "200,404"
  }

  tags = {
    Name = "${var.service_name}-tweet-tg"
  }
}

resource "aws_lb_target_group" "timeline" {
  name     = "${var.service_name}-timeline-tg"
  port     = 8083
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id
  target_type = "ip"

  health_check {
    enabled             = true
    healthy_threshold   = 2
    unhealthy_threshold = 2
    timeout             = 5
    interval            = 30
    path                = "/health"
    matcher             = "200,404"
  }

  tags = {
    Name = "${var.service_name}-timeline-tg"
  }
}

# ALB Listener - Routes requests based on path patterns
resource "aws_lb_listener" "this" {
  load_balancer_arn = aws_lb.this.arn
  port              = "80"
  protocol          = "HTTP"

  # Default action - Route to gateway
  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.gateway.arn
  }
}

# Listener Rules - Path-based routing for microservices
resource "aws_lb_listener_rule" "user_auth" {
  listener_arn = aws_lb_listener.this.arn
  priority     = 100

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.user.arn
  }

  condition {
    path_pattern {
      values = ["/internal/user/*"]
    }
  }
}

resource "aws_lb_listener_rule" "tweet_internal" {
  listener_arn = aws_lb_listener.this.arn
  priority     = 200

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.tweet.arn
  }

  condition {
    path_pattern {
      values = ["/internal/tweet/*"]
    }
  }
}

resource "aws_lb_listener_rule" "timeline_internal" {
  listener_arn = aws_lb_listener.this.arn
  priority     = 300

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.timeline.arn
  }

  condition {
    path_pattern {
      values = ["/internal/timeline/*"]
    }
  }
}
