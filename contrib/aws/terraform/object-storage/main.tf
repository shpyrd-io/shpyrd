# Kept outside the cluster's Terraform state so destroying the cluster does
# not remove the gateway's data or durable consumer descriptors.
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.0"
    }
  }
}
variable "name" {
  type    = string
  default = "shpyrd-dev"
}
variable "region" {
  type    = string
  default = "us-east-1"
}
provider "aws" {
  region = var.region
}
data "aws_caller_identity" "current" {}
resource "aws_s3_bucket" "objects" {
  bucket = "${var.name}-objects-${data.aws_caller_identity.current.account_id}"
}
resource "aws_s3_bucket_public_access_block" "objects" {
  bucket                  = aws_s3_bucket.objects.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
resource "aws_s3_bucket_server_side_encryption_configuration" "objects" {
  bucket = aws_s3_bucket.objects.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}
resource "aws_s3_bucket_lifecycle_configuration" "objects" {
  bucket = aws_s3_bucket.objects.id
  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {
      prefix = "buckets/"
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}
resource "aws_iam_user" "gateway" {
  name = "${var.name}-object-gateway"
}
resource "aws_iam_user_policy" "gateway" {
  user = aws_iam_user.gateway.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:ListBucket", "s3:GetBucketLocation", "s3:ListBucketMultipartUploads"]
        Resource = aws_s3_bucket.objects.arn
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"]
        Resource = "${aws_s3_bucket.objects.arn}/*"
      }
    ]
  })
}
resource "aws_iam_access_key" "gateway" {
  user = aws_iam_user.gateway.name
}
resource "local_sensitive_file" "credentials" {
  filename        = "${path.module}/${var.name}-objects.env"
  file_permission = "0600"
  content         = <<-EOT
    AWS_ACCESS_KEY_ID=${aws_iam_access_key.gateway.id}
    AWS_SECRET_ACCESS_KEY=${aws_iam_access_key.gateway.secret}
    SHPYRD_GATEWAY_REGION=${var.region}
    SHPYRD_GATEWAY_BUCKET=${aws_s3_bucket.objects.bucket}
  EOT
}
output "object_storage_credentials_file" {
  value = abspath(local_sensitive_file.credentials.filename)
}
output "offline_restore_target" {
  value = "s3://${aws_s3_bucket.objects.bucket}/buckets/platform-backups/platform"
}
