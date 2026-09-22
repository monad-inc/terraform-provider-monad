# External enrichment providers take an API key under config.secrets, keyed by
# the field name on that enrichment's docs page. Reference an existing secret
# ({ id }) or create one inline ({ value, name, description }).
resource "monad_secret" "greynoise" {
  name  = "greynoise-api-key"
  value = var.greynoise_api_key
}

resource "monad_enrichment" "ip_reputation" {
  name = "GreyNoise IP context"
  type = "greynoise-community"

  config {
    settings = jsondecode(jsonencode({
      ip_address_path  = "source.ip"
      destination_path = "enrichment.greynoise"
    }))

    secrets = {
      api_key = { id = monad_secret.greynoise.id }
    }
  }
}
