# Rotate by moving the expiration. Changing expiration_time regenerates the key
# in place: the id stays the same, `token` changes, and the previous token stops
# working as soon as the apply finishes. Update whatever consumes the token in
# the same apply.
#
# time_rotating (hashicorp/time) moves its timestamp every 30 days, so each
# apply after that point rotates the key. The new expiration is only known at
# apply time; the plan shows `token` as known after apply.
data "monad_role" "reader" {
  name = "Reader"
}

resource "time_rotating" "api_key" {
  rotation_days = 30
}

resource "monad_api_key" "rotated" {
  name            = "nightly-export"
  role_id         = data.monad_role.reader.id
  expiration_time = timeadd(time_rotating.api_key.rfc3339, "1440h") # 60 days: a 30-day overlap
}
