resource "monad_input" "input" {
  name        = "Crowdstrike Vulnerabilities (Event Generator Example)"
  description = "This input uses synthetic data events to mimic CrowdStrike vulnerability data and provide data to test against"
  type        = "demo"

  config {
    settings = {
      record_type = "crowdstrike_vulnerabilities"
      rate        = 1
    }
  }
}