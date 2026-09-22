# The value comes from a variable (or TF_VAR_*, or a secrets-manager data
# source) and is write-only: it is sent to Monad but never written to state.
variable "archive_secret_access_key" {
  type      = string
  sensitive = true
}

resource "monad_secret" "archive_secret_key" {
  name        = "archive-bucket-secret-key"
  description = "AWS secret access key for the archive bucket writer"
  value       = var.archive_secret_access_key
}
