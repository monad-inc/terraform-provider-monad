# Create a pipeline disabled, then flip enabled once secrets are populated and
# a test run looks right. enabled is optional; omitting it adopts the server
# default (enabled) rather than forcing a value.
resource "monad_pipeline" "staged" {
  name    = "CloudTrail (staged)"
  enabled = false

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.archive.id
  }
  nodes {
    slug           = "archive"
    component_type = "output"
    component_id   = monad_output.archive.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "archive"
    condition {
      operator = "always"
    }
  }
}
