# A pull connector with settings that reference other Terraform values. Every
# key under settings is the connector's API field name — see the "API Examples"
# section of that connector's page in the Monad docs.
resource "monad_input" "archive" {
  name        = "CloudTrail archive (S3)"
  description = "Reads NDJSON CloudTrail objects from the ingest bucket"
  type        = "s3"

  config {
    settings = jsondecode(jsonencode({
      bucket              = var.ingest_bucket
      region              = "us-west-2"
      prefix              = "cloudtrail"
      role_arn            = var.ingest_role_arn
      compression         = "none"
      format              = "jsonl"
      partition_format    = "simple date"
      backfill_start_time = "2026-01-01T00:00:00Z"
    }))
  }
}
