# Rotate by moving the expiration. Changing expiration_time regenerates the key
# in place: the id stays the same, `token` changes, and the previous token stops
# working as soon as the apply finishes. Update whatever consumes the token in
# the same apply.
data "monad_role" "reader" {
  name = "Reader"
}

resource "monad_api_key" "rotated" {
  name            = "nightly-export"
  role_id         = data.monad_role.reader.id
  expiration_time = timeadd(var.rotation_epoch, "2160h") # 90 days after the last rotation
}
