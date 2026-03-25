# RDS outputs - Updated to include replica endpoints
output "endpoint" {
  description = "Primary database endpoint"
  value = aws_db_instance.this.address
}

output "port" {
  description = "Database port"
  value = aws_db_instance.this.port
}

output "db_name" {
  description = "Database name"
  value = aws_db_instance.this.db_name
}

output "replica_endpoints" {
  description = "Read replica endpoints for load distribution"
  value = [
    aws_db_instance.replica1.address,
    aws_db_instance.replica2.address
  ]
}

output "primary_connection_string" {
  description = "PostgreSQL connection string for primary"
  value = "postgres://${aws_db_instance.this.username}:${aws_db_instance.this.password}@${aws_db_instance.this.address}:${aws_db_instance.this.port}/${aws_db_instance.this.db_name}"
  sensitive = true
}

output "replica_connection_strings" {
  description = "PostgreSQL connection strings for replicas"
  value = [
    "postgres://${aws_db_instance.this.username}:${aws_db_instance.this.password}@${aws_db_instance.replica1.address}:${aws_db_instance.replica1.port}/${aws_db_instance.this.db_name}",
    "postgres://${aws_db_instance.this.username}:${aws_db_instance.this.password}@${aws_db_instance.replica2.address}:${aws_db_instance.replica2.port}/${aws_db_instance.this.db_name}"
  ]
  sensitive = true
}
