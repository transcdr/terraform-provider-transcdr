# S3 → SNS → HTTPS → Transcdr hook

New files in a bucket publish to an SNS topic. The topic has an HTTPS subscription to a `hook` automation's secret `hook_url`, and other consumers (a queue, a Lambda, email) can subscribe to the same topic.

```
upload to s3://<bucket>/incoming/…
  └─ S3 event notification ─▶ SNS topic ─┬─▶ HTTPS: transcdr_automation.hook.hook_url ─▶ job
                                          └─▶ your other subscribers
```

Transcdr confirms the SNS subscription by itself. It fetches `SubscribeURL`, but only from `sns.<region>.amazonaws.com`. It also unwraps the SNS envelope and decodes the S3 key.

## What it creates

- `aws_s3_bucket.media`, `aws_sns_topic.uploads`
- `data.transcdr_bucket_automation_setup.this` with `method = "webhook"` and `topic_arn`. It generates:
  - the topic policy that lets S3 publish (`aws_sns_topic_policy`)
  - the notification filters (`aws_s3_bucket_notification` with `topic` blocks)
  - the read-only IAM policy for Transcdr's keys
- `transcdr_connection.bucket` (`s3`) and `transcdr_automation.hook` (`trigger = "hook"`). Outputs stay in Transcdr's storage.
- `aws_sns_topic_subscription.transcdr`: `protocol = "https"`, `endpoint = transcdr_automation.hook.hook_url`.

## Use it

```sh
export TRANSCDR_API_KEY=tdk_live_…   # connections:write, automations:write
terraform init
terraform apply
aws s3 cp talk.mp4 "$(terraform output -raw bucket)talk.mp4"
```

## Rotating the hook URL

The hook URL is the credential. To rotate it, change `hook_token_version` (a variable here) and apply. Transcdr issues a new URL and the old one stops working. Terraform then replaces the subscription, since its endpoint changed, and Transcdr confirms the new one.
