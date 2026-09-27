# S3 → SQS → Transcdr → S3

New videos uploaded to an S3 bucket are transcoded automatically with the pipeline's own preset, and the outputs are delivered back to the same bucket. Completions are published to an SNS topic.

```
upload to s3://<bucket>/incoming/…
  └─ S3 event notification ─▶ SQS queue (with a dead-letter queue)
                                 └─ Transcdr reads the queue (queue automation)
                                      └─ job with the preset ─▶ outputs to s3://<bucket>/transcoded/<name>/
                                      └─ job.completed / job.failed ─▶ SNS topic
```

Everything is declared in one module: the bucket, its notification, the queue, the IAM user, the Transcdr connections, the preset, the automation and the event destination. A change to any of them is a plan and an apply.

This is the recommended setup on AWS. Nothing needs a public endpoint, and if anything is down the backlog waits in the queue.

## What it creates

| Resource | Why |
|---|---|
| `aws_s3_bucket.media` | Holds the source videos and the outputs. |
| `aws_sqs_queue.triggers` and `aws_sqs_queue.dlq` | The trigger queue, plus a dead-letter queue for messages that fail 5 times. |
| `data.transcdr_bucket_automation_setup.this` | Generates the policies and the notification filters, matching the dashboard's *Automate a bucket*. It runs locally and makes no API call. |
| `aws_sqs_queue_policy.triggers` | Lets S3 send the bucket's events to the queue. |
| `aws_s3_bucket_notification.media` | Sends `s3:ObjectCreated:*` to the queue. There is one configuration per extension in the pattern, because S3 suffix filters are single and case-sensitive. |
| `aws_iam_user.transcdr` with its access key and policies | Transcdr's credentials: list, read and write the bucket, consume the queue, and publish to the topic. |
| `transcdr_connection.bucket` (`s3`) and `transcdr_connection.queue` (`sqs`) | The bucket and the queue, as Transcdr sees them. |
| `transcdr_preset.delivery` | The pipeline's preset: an HLS ladder (1080p, 720p, 480p) in `var.codec`. |
| `transcdr_automation.ingest` | `trigger = "queue"`: each notification becomes a job using the preset, delivered back to the bucket. |
| `transcdr_event_destination.completions` | Sends `job.completed` and `job.failed` to the SNS topic, signed with `signing_secret`. |

## Use it

```sh
export TRANSCDR_API_KEY=tdk_live_…   # connections:write, presets:write, automations:write, webhooks:write
export AWS_PROFILE=…                 # or any other AWS credentials
terraform init
terraform apply

aws s3 cp talk.mov "$(terraform output -raw bucket)talk.mov"
```

The job appears in the dashboard within seconds. When it completes, its outputs land in `s3://<bucket>/transcoded/talk/`.

Variables: `region`, `name`, `prefix` (default `incoming/`), `pattern`, `codec` (default `h264`), `output_prefix` (default `transcoded/{stem}/`, which must be outside `prefix`), `notification_email`, and `iam_propagation_wait`.

## Testing against LocalStack

The provider's acceptance tests apply this module unchanged, with a `localstack_override.tf` that points the AWS provider and the connections at LocalStack. The test then uploads a file, checks that the automation turned it into a job with the preset, and checks that a second plan is empty. See `internal/provider/example_e2e_test.go`.
