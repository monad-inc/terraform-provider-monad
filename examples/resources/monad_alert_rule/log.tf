# Real-time match on pipeline log events. At least one of levels or
# message_filter is required; operator and value must be set together.
resource "monad_alert_rule" "schema_mismatch_logged" {
  name     = "Schema mismatch logged"
  type     = "monad-log-alert"
  severity = "medium"

  rule_config = jsondecode(jsonencode({
    settings = {
      log_type = "pipeline"
      levels   = ["error", "fatal"]
      message_filter = {
        operator = "contains"
        value    = "schema mismatch"
      }
      dedupe_window = "30m"
    }
  }))
}
