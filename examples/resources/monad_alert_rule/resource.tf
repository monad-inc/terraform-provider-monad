# Fire when a pipeline ingests more than 10,000 records in five minutes.
# rule_config is free-form: each alert type has its own settings schema, which
# the API validates on write. Build it with jsondecode(jsonencode({...})).
resource "monad_alert_rule" "ingest_spike" {
  name        = "CloudTrail — ingest volume spike"
  description = "More than 10,000 records ingested in a 5-minute window"
  type        = "threshold-alert"
  severity    = "critical"

  pipeline_ids = [monad_pipeline.basic.id]

  rule_config = jsondecode(jsonencode({
    settings = {
      metric_config = {
        type = "records"
        records = {
          direction = "ingress"
          threshold = 10000
        }
      }
      operator    = "greater_than"
      time_window = "5m"
    }
  }))
}
