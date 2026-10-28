data "monad_alert_rule_type" "threshold" {
  type_id = "threshold-alert"
}

# The allowed time windows and operators.
output "threshold_choices" {
  value = {
    for f in data.monad_alert_rule_type.threshold.settings : f.path => f.allowed_values
    if length(f.allowed_values) > 0
  }
}
