# The simplest output: discard everything. Useful as a placeholder sink while
# a pipeline is being built.
resource "monad_output" "sink" {
  name        = "Discard"
  description = "dev-null sink — swap type and settings for a real destination"
  type        = "dev-null"
}
