data "monad_output_type" "kv" {
  type_id = "kv-lookup"
}

output "kv_lookup_fields" {
  value = { for f in data.monad_output_type.kv.settings : f.path => f.description }
}
