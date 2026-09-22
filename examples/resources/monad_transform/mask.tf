# Operations that need key material reference a secret by ID, exactly like a
# connector does. Here the mask operation's deterministic mode hashes the value
# under _dedup_key with an HMAC key held in a monad_secret.
resource "monad_secret" "dedup_hmac_key" {
  name        = "dedup-hmac-key"
  description = "HMAC key for the deterministic dedup mask"
  value       = var.dedup_hmac_key
}

resource "monad_transform" "fingerprint" {
  name = "Fingerprint record"

  config = jsondecode(jsonencode({
    operations = [
      {
        operation = "jq"
        arguments = {
          key   = "_dedup_key"
          query = "del(.ingested_at) | tojson"
        }
      },
      {
        operation = "mask"
        arguments = {
          key = "_dedup_key"
          mode = {
            type = "deterministic"
            deterministic = {
              hash_key = { id = monad_secret.dedup_hmac_key.id }
            }
          }
        }
      },
    ]
  }))
}
