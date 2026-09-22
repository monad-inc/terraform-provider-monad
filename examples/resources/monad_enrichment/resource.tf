# Joins each record against a KV Lookup output. The enrichment references the
# output by ID, so Terraform orders the creates correctly.
resource "monad_output" "asset_inventory" {
  name = "Asset inventory (KV)"
  type = "kv-lookup"

  config {
    settings = {
      key_field = "asset_id" # omit value_field to store the whole record
      ttl       = 172800
    }
  }
}

resource "monad_enrichment" "asset_context" {
  name        = "Asset context"
  description = "Adds owner and criticality from the asset inventory table"
  type        = "kv-lookup"

  config {
    settings = {
      kv_lookup_output_id  = monad_output.asset_inventory.id
      join_path            = "asset.id"
      destination_key      = "enrichment.asset"
      error_on_missing_key = false
    }
  }
}
