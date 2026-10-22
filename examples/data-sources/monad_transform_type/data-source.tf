data "monad_transform_type" "rename" {
  operation = "rename_key"
}

output "rename_key_arguments" {
  value = [for f in data.monad_transform_type.rename.arguments : "${f.path}${f.required ? " (required)" : ""}"]
}
