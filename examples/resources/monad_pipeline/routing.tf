# Fan-out with conditional routing. A node may have many outgoing edges, each
# with its own condition; a record follows every edge whose condition matches
# and is dropped if none does. The root operator is always a logical operator
# (always, never, and, or, nor, xor) wrapping one or more leaf conditions.
resource "monad_pipeline" "routed" {
  name    = "CloudTrail routed by severity"
  enabled = true

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "normalize"
    component_type = "transform"
    component_id   = monad_transform.cloudtrail_to_ecs.id
  }
  nodes {
    slug           = "asset-context"
    component_type = "enrichment"
    component_id   = monad_enrichment.asset_context.id
  }
  nodes {
    slug           = "siem"
    component_type = "output"
    component_id   = monad_output.siem.id
  }
  nodes {
    slug           = "archive"
    component_type = "output"
    component_id   = monad_output.archive.id
  }
  nodes {
    slug           = "sample"
    component_type = "output"
    component_id   = monad_output.sample_bucket.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "normalize"
    condition {
      operator = "always"
    }
  }
  edges {
    from_node_instance_slug = "normalize"
    to_node_instance_slug   = "asset-context"
    condition {
      operator = "always"
    }
  }

  # Everything goes to the archive.
  edges {
    from_node_instance_slug = "asset-context"
    to_node_instance_slug   = "archive"
    condition {
      operator = "always"
    }
  }

  # Only failures and console logins from outside the corporate ranges reach
  # the SIEM: (error_code exists OR event is ConsoleLogin) AND NOT internal.
  edges {
    name                    = "high value to SIEM"
    from_node_instance_slug = "asset-context"
    to_node_instance_slug   = "siem"

    condition {
      operator = "and"

      conditions {
        type_id = "equals_any"
        config {
          key    = "event.action"
          values = ["ConsoleLogin", "AssumeRole", "CreateAccessKey"]
        }
      }
      conditions {
        type_id = "starts_with"
        config {
          key   = "source.ip"
          value = "10."
          not   = true
        }
      }
    }
  }

  # A 5% sample of everything else, for tuning detections offline.
  edges {
    name                    = "sample for tuning"
    from_node_instance_slug = "asset-context"
    to_node_instance_slug   = "sample"

    condition {
      operator = "and"
      conditions {
        type_id = "sample"
        config {
          percent = 5
        }
      }
    }
  }
}
