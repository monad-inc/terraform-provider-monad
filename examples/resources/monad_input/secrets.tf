# Credentials go under config.secrets, keyed by the connector's secret field
# names. Each value is either a reference to an existing secret ({ id }) or a
# new secret created inline ({ value, name, description } — all three required).
# The block is write-only: nothing under secrets is ever stored in state.
resource "monad_secret" "aws_secret_key" {
  name  = "s3-archive-secret-key"
  value = var.aws_secret_access_key
}

resource "monad_input" "archive_static_creds" {
  name = "CloudTrail archive (S3, static credentials)"
  type = "s3"

  config {
    settings = jsondecode(jsonencode({
      bucket = var.ingest_bucket
      region = "us-west-2"
      prefix = "cloudtrail"
      format = "jsonl"
    }))

    secrets = {
      # Reference a secret managed elsewhere in this configuration.
      secret_key = { id = monad_secret.aws_secret_key.id }

      # Or create one inline. The secret is stored by name, so rotate the
      # value by giving it a new name too.
      access_key = {
        name        = "s3-archive-access-key"
        description = "Access key ID for the archive bucket reader"
        value       = var.aws_access_key_id
      }
    }
  }
}
