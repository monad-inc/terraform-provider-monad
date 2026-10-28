# A condition group: route console logins and role assumptions that carry a
# user identity. The edge's own operator (`and`) combines a group (`or` over two
# `equals` rules) with a plain rule. An element sets either `type_id` (a rule)
# or `operator` (a group); a group's `conditions` hold rules only, because the
# API caps a condition at three levels.
resource "monad_pipeline" "grouped" {
  name = "CloudTrail sign-in activity"

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
      operator = "and"
      conditions {
        operator = "or"
        conditions {
          type_id = "equals"
          config {
            key   = "eventName"
            value = "ConsoleLogin"
          }
        }
        conditions {
          type_id = "equals"
          config {
            key   = "eventName"
            value = "AssumeRole"
          }
        }
      }
      conditions {
        type_id = "key_exists"
        config {
          key = "userIdentity"
        }
      }
    }
  }
}
