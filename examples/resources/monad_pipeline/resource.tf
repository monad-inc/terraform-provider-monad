# The smallest useful pipeline: input → transform → output. Nodes name the
# components; edges connect nodes by slug. An "always" condition passes every
# record.
resource "monad_pipeline" "basic" {
  name        = "CloudTrail to archive"
  description = "Tags each record, then archives it"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "tag"
    component_type = "transform"
    component_id   = monad_transform.tag_and_trim.id
  }
  nodes {
    slug           = "archive"
    component_type = "output"
    component_id   = monad_output.archive.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "tag"
    condition {
      operator = "always"
    }
  }
  edges {
    from_node_instance_slug = "tag"
    to_node_instance_slug   = "archive"
    condition {
      operator = "always"
    }
  }
}
