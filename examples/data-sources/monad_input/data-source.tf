data "monad_input" "cloudtrail" {
  name = "CloudTrail (production)"
}

# Route an input managed in another repository into a pipeline managed here.
resource "monad_pipeline" "archive" {
  name = "CloudTrail archive"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = data.monad_input.cloudtrail.id
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
