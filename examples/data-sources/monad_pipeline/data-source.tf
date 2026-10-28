data "monad_pipeline" "cloudtrail" {
  name = "CloudTrail routed by severity"
}

# Watch a pipeline managed elsewhere.
resource "monad_alert_rule" "stalled" {
  name         = "CloudTrail ingest stalled"
  type         = "threshold-alert"
  severity     = "high"
  pipeline_ids = [data.monad_pipeline.cloudtrail.id]
  rule_config = {
    settings = {
      metric_config = {
        type    = "records"
        records = { direction = "ingress", threshold = 1 }
      }
      operator    = "less_than"
      time_window = "30m"
    }
  }
}
