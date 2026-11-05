data "monad_input_type" "demo" {
  type_id = "demo"
}

# Every setting the type requires.
output "demo_required_settings" {
  value = [for f in data.monad_input_type.demo.settings : f.path if f.required]
}
