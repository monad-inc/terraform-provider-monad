# Schema drift detection is per edge and off unless the block is present.
# Declare it on every edge where detection should stay on; omitting the block
# on a later apply turns detection off and discards the learned schema.
resource "monad_pipeline" "monitored" {
  name = "CloudTrail (schema-monitored)"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "siem"
    component_type = "output"
    component_id   = monad_output.siem.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "siem"

    condition {
      operator = "always"
    }

    schema_detection_spec {
      enabled = true
      # Learn and report drift, but do not raise alerts for this edge.
      disable_alerting = true
    }
  }
}
