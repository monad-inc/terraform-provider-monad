data "monad_secret" "shared" {
  name = "shared-siem-token"
}

# Hand the ID to a connector managed elsewhere, or reference it inline:
#   secrets = { <field> = { id = data.monad_secret.shared.id } }
# using the field name from that connector's docs page.
output "shared_secret_id" {
  value = data.monad_secret.shared.id
}
