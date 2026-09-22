# Sustained Erroring or Throttled status. An empty pipeline_ids watches every
# pipeline in the organization.
resource "monad_alert_rule" "erroring" {
  name     = "Pipeline erroring for 5 minutes"
  type     = "pipeline-status-alert"
  severity = "high"
  active   = true

  rule_config = jsondecode(jsonencode({
    settings = {
      status      = "Erroring"
      time_window = "5m"
    }
  }))
}
