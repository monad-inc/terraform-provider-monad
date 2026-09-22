# The bytes metric adds a unit. Watching egress fall BELOW a floor catches a
# destination that has gone quiet.
resource "monad_alert_rule" "egress_stalled" {
  name     = "SIEM egress stalled"
  type     = "threshold-alert"
  severity = "high"

  pipeline_ids = [monad_pipeline.routed.id]

  rule_config = jsondecode(jsonencode({
    settings = {
      metric_config = {
        type = "bytes"
        bytes = {
          direction = "egress"
          threshold = 1
          unit      = "MB"
        }
      }
      operator    = "less_than"
      time_window = "1h"
    }
  }))
}
