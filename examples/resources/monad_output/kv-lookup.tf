# A KV Lookup output is a key-value table other pipelines can read from via a
# monad_enrichment of type "kv-lookup". ttl is the entry lifetime in seconds.
resource "monad_output" "dedup_store" {
  name        = "Dedup fingerprints (KV)"
  description = "Record fingerprints already shipped; 48h TTL is the dedup window"
  type        = "kv-lookup"

  config {
    settings = {
      key_field   = "_dedup_key"
      value_field = "_dedup_key"
      ttl         = 172800
    }
  }
}
