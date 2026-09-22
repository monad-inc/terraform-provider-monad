# Synthetic CloudTrail events, one record per second. The "demo" input needs
# no credentials, so it is the quickest way to get data flowing.
resource "monad_input" "demo" {
  name        = "CloudTrail (synthetic)"
  description = "Event Generator producing CloudTrail-shaped records for testing"
  type        = "demo"

  config {
    settings = {
      record_type = "cloudtrail"
      rate        = 1
    }
  }
}
