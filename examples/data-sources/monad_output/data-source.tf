data "monad_output" "siem" {
  name = "Production SIEM"
}

output "siem_feeds" {
  description = "Pipelines that write to the SIEM"
  value       = data.monad_output.siem.pipeline_ids
}
