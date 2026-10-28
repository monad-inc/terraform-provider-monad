# An API key for a CI job that only sends data. Pick the narrowest role that
# works; the API refuses to grant a key permissions the caller lacks.
resource "monad_api_key" "ci_sender" {
  name            = "ci-data-sender"
  description     = "Used by the ingest job in CI"
  role_id         = var.data_sender_role_id
  expiration_time = "2027-01-01T00:00:00Z"
}

# The token is returned only when the key is created or regenerated, so the
# provider keeps it in state (sensitive). Pass it on from here, for example
# into your CI system's secret store.
output "ci_sender_token" {
  value     = monad_api_key.ci_sender.token
  sensitive = true
}
