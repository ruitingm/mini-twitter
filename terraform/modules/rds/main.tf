resource "aws_db_subnet_group" "this" {
  name       = "${var.service_name}-db-subnet-group"
  subnet_ids = var.subnet_ids

  tags = {
    Name = "${var.service_name}-db-subnet-group"
  }
}

# Security group - Updated for PostgreSQL port 5432
resource "aws_security_group" "rds_sg" {
  name   = "${var.service_name}-rds-sg"
  vpc_id = var.vpc_id

  # Allow PostgreSQL from ECS
  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [var.ecs_security_group_id]
  }

  # Allow PostgreSQL from anywhere (for migrations and seeding)
  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# Primary PostgreSQL database - Updated from MySQL
resource "aws_db_instance" "this" {
  identifier = "${var.service_name}-postgres-primary"

  engine         = "postgres"
  engine_version = "16.6"
  instance_class = "db.t3.micro"

  allocated_storage = 20
  storage_type      = "gp2"

  db_name  = var.db_name
  username = var.db_username
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds_sg.id]

  publicly_accessible = true

  skip_final_snapshot     = true
  deletion_protection     = false
  backup_retention_period = 0
  maintenance_window     = "sun:04:00-sun:05:00"

  multi_az = false

  tags = {
    Name = "${var.service_name}-postgres-primary"
  }
}

