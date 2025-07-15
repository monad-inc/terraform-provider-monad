resource "monad_enrichment" "test-enrichment" {
  name        = "first enrichment"
  description = "first enrichment example"
  type        = "kv-lookup"

  config {
    settings = {
      destination_key  = "."
      error_on_missing_key = false
      join_key = "key"
      kv_lookup_output_id = "test"
    }
  }
}