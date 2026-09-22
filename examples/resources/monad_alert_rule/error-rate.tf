# Error rate as a percentage of ingested records, with a floor on record count
# so quiet pipelines do not false-alarm.
resource "monad_alert_rule" "error_rate" {
  name     = "Error rate above 5%"
  type     = "error-rate-alert"
  severity = "medium"

  pipeline_ids = [monad_pipeline.basic.id, monad_pipeline.routed.id]

  rule_config = jsondecode(jsonencode({
    settings = {
      threshold   = 5.0
      time_window = "1h"
      min_records = 100
    }
  }))
}
