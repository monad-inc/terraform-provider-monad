# Organization-level alert types take no pipeline_ids.
resource "monad_alert_rule" "budget" {
  name     = "Monthly spend above $5,000"
  type     = "billing-metrics-cost-budget"
  severity = "high"

  rule_config = {
    settings = {
      usd_amount = 5000
    }
  }
}
