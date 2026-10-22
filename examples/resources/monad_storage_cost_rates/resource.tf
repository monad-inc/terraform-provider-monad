# Price storage by output type, and give one SIEM output its own rate.
# Only the types and outputs listed here are managed; rates set elsewhere in
# the organization are left alone.
resource "monad_storage_cost_rates" "this" {
  costs = {
    "dev-null"  = 0
    "kv-lookup" = 0.10
  }

  output_costs = {
    (monad_output.siem.id) = {
      storage_type = "dev-null"
      cost_per_gb  = 2.75
    }
  }
}
