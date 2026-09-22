# One edge showing each leaf rule. Every rule reads `key`, and most accept
# `not`; the remaining fields are rule-specific and the provider rejects a
# mismatch at plan time.
resource "monad_pipeline" "every_rule" {
  name = "Condition rule reference"

  nodes {
    slug           = "source"
    component_type = "input"
    component_id   = monad_input.demo.id
  }
  nodes {
    slug           = "sink"
    component_type = "output"
    component_id   = monad_output.sink.id
  }

  edges {
    from_node_instance_slug = "source"
    to_node_instance_slug   = "sink"

    condition {
      # "nor" passes a record only when NONE of the leaves match.
      operator = "nor"

      conditions {
        type_id = "equals"
        config {
          key              = "event.outcome"
          value            = "success"
          case_insensitive = true
        }
      }
      conditions {
        type_id = "contains"
        config {
          key   = "user.name"
          value = "svc-"
          raw   = true
        }
      }
      conditions {
        type_id = "ends_with"
        config {
          key   = "user.email"
          value = "@example.com"
        }
      }
      conditions {
        type_id = "greater_than"
        config {
          key   = "http.response.status_code"
          value = "499"
        }
      }
      conditions {
        type_id = "less_than"
        config {
          key   = "event.duration"
          value = "10"
        }
      }
      conditions {
        type_id = "matches_regex"
        config {
          key     = "source.ip"
          pattern = "^(10|192\\.168)\\."
        }
      }
      conditions {
        type_id = "key_exists"
        config {
          key = "error.code"
        }
      }
      conditions {
        type_id = "is_empty"
        config {
          key               = "user.id"
          null              = true
          whitespace_string = true
        }
      }
    }
  }
}
