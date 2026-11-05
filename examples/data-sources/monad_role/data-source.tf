# Reference a built-in role without hard-coding its per-organization ID.
data "monad_role" "data_sender" {
  name = "Data Sender"
}

resource "monad_api_key" "ingest" {
  name            = "ingest"
  role_id         = data.monad_role.data_sender.id
  expiration_time = "2027-01-01T00:00:00Z"
}
