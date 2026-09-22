# templatefile() lets a jq program embed values from other resources — here a
# pipeline-id → name map — so a rebuild updates the transform in place.
resource "monad_transform" "label_alerts" {
  name = "Label alerts with pipeline names"

  config = {
    operations = [
      {
        operation = "jq"
        arguments = {
          key = ""
          query = templatefile("${path.module}/jq/label-alerts.jq.tftpl", {
            pipelines_json = jsonencode({
              (monad_pipeline.cloudtrail.id) = monad_pipeline.cloudtrail.name
              (monad_pipeline.archive.id)    = monad_pipeline.archive.name
            })
          })
        }
      },
    ]
  }
}
