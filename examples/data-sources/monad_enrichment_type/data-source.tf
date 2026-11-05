data "monad_enrichment_type" "kv" {
  type_id = "kv-lookup"
}

output "kv_enrichment_settings" {
  value = [for f in data.monad_enrichment_type.kv.settings : f.path]
}
