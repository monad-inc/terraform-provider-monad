# An object-storage destination with nested settings (format and batching).
resource "monad_output" "archive" {
  name        = "CloudTrail archive (S3)"
  description = "Durable archive of every ingested record, unmodified"
  type        = "s3"

  config {
    settings = {
      role_arn         = var.archive_role_arn
      bucket           = var.archive_bucket
      region           = "us-west-2"
      prefix           = "archive"
      compression      = "none"
      partition_format = "simple date"
      format_config = {
        Format      = "json"
        json_format = { type = "line" }
      }
      batch_config = {
        batch_record_count = 500
        batch_data_size    = 1048576
        publish_rate       = 5
      }
    }
  }
}
