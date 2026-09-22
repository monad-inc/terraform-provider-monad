# Some connectors take their credentials inside settings as a secret reference
# ({ id = ... }) rather than under config.secrets. The connector's docs page
# shows which; the shape below is the Slack output's webhook variant.
resource "monad_secret" "slack_webhook" {
  name  = "slack-alerts-webhook"
  value = var.slack_webhook_url
}

resource "monad_output" "slack" {
  name        = "Slack #security-alerts"
  description = "Posts fired and resolved Monad alerts to Slack"
  type        = "slack"

  config {
    settings = {
      auth_config = {
        type = "webhook"
        webhook = {
          webhook_url = { id = monad_secret.slack_webhook.id }
        }
      }
      message_template = file("${path.module}/slack-alert-template.tmpl")
    }
  }
}
