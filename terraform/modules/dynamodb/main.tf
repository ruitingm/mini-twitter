resource "aws_dynamodb_table" "this" {
  name         = "${var.service_name}-cart-table"
  billing_mode = "PAY_PER_REQUEST"

  # hash(cart_id) → exact partition → fetch item
  hash_key = "cart_id"

  attribute {
    name = "cart_id"
    type = "S"
  }

  tags = {
    Name = "${var.service_name}-dynamodb"
  }
}
