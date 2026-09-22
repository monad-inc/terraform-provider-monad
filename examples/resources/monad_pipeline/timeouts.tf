# Pipeline creation is serialized on the API side, so a large apply can push
# individual creates past the provider's request_timeout. Give this resource
# its own budget rather than raising the provider-wide one.
resource "monad_pipeline" "big_fanout" {
  name = "CloudTrail fan-out"

  timeouts {
    create = "10m"
    update = "10m"
  }

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
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
