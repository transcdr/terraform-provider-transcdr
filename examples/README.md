# Terraform examples

This folder holds working Terraform for the Transcdr provider:
- four complete pipelines you can apply as they are
- the short snippets shown on each resource's page in the provider docs

Every file is commented line by line: what each resource does, why it is there, and what to watch out for.

## Choose an example

| Example | You have | New files reach Transcdr by | Outputs go to | Also creates |
|---|---|---|---|---|
| [`s3-sqs-queue-automation`](s3-sqs-queue-automation) | AWS | S3 notification → **SQS queue** → Transcdr reads the queue | the same S3 bucket | its own preset, and an SNS topic for completion events |
| [`s3-sns-fanout-hook`](s3-sns-fanout-hook) | AWS | S3 notification → **SNS topic** → HTTPS push to the automation's hook URL | Transcdr's storage | a topic other consumers can subscribe to |
| [`r2-watch-folder`](r2-watch-folder) | Cloudflare R2 | Transcdr **lists the folder** every few minutes | the same R2 bucket | |
| [`presets-and-destinations`](presets-and-destinations) | only a Transcdr account | (no automation) | | presets, including constant bit rate, and an HTTPS event destination |

**Which trigger?**
- **Queue** (AWS): events arrive in seconds, Transcdr needs no public endpoint, and nothing is lost while either side is down. This is the recommended setup on AWS.
- **Hook**: also seconds, but whatever sends the events has to reach Transcdr's public hook URL. Use it with SNS when other systems need the same events, with MinIO, which posts webhooks natively, or with your own code.
- **Watch**: works with any storage, including R2, B2 and FTP, and needs only read access. A new file waits up to one poll interval, plus the settle time, before its job starts.

## Before you start

1. **Terraform** 1.5 or newer.
2. **A Transcdr API key.** Create one in the dashboard under *Settings → API keys* and export it:
   ```sh
   export TRANSCDR_API_KEY=tdk_live_…
   ```
   A test-mode key (`tdk_test_…`) works for trying things out: its jobs finish in seconds with placeholder outputs, and are free. Each example's `versions.tf` lists the scopes it needs.
3. **Your cloud account**, for the examples that use one:
   - **AWS:** any credentials the AWS provider accepts (`AWS_PROFILE`, access keys, SSO). They need to create buckets, queues, topics and IAM users. Transcdr gets its own IAM user, limited to the bucket, queue and topic it uses.
   - **Cloudflare:** `CLOUDFLARE_API_TOKEN` with R2 edit access, plus an R2 API token for Transcdr (see the example's README).
4. **Plan:** connections and automations need the Starter plan or above, and event destinations need the `webhooks` feature. The `transcdr_organization` data source reports both.

## Run one

```sh
cd examples/s3-sqs-queue-automation
terraform init
terraform plan          # review what will be created
terraform apply
terraform output        # where to upload, ids, and so on
```

Override defaults with `-var name=value`, or in a `terraform.tfvars` file next to the example. Keep that file out of version control if it holds credentials: `*.tfvars` is ignored in this repository.

**Clean up** with `terraform destroy`. The example buckets use `force_destroy = true`, so destroying them deletes the videos and outputs in them too.

## What an example looks like

Each complete example is a standalone root module with the same layout:

| File | Contains |
|---|---|
| `versions.tf` | the required providers and their versions, and the provider blocks (credentials come from the environment) |
| `variables.tf` | every input, with a default wherever one makes sense, and a description of what it changes |
| `main.tf` | the resources, in dependency order, with a header explaining the whole flow |
| `outputs.tf` | what you need afterwards: where to upload, ids, the signing secret (sensitive) |
| `README.md` | a diagram, the resources and how to use it |

Copy a folder to start your own configuration. Or call one as a module from yours: `module "ingest" { source = "./s3-sqs-queue-automation" ... }`. In that case, remove its `provider` blocks and pass the providers in from your configuration.

## How the pieces fit together

```
        your bucket                          Transcdr
 ┌─────────────────────┐        ┌────────────────────────────────────┐
 │ incoming/talk.mov   │──(1)──▶│ transcdr_connection (storage)      │
 │                     │        │   credentials, tested when saved   │
 │ transcoded/talk/…   │◀──(4)──│                                    │
 └─────────────────────┘        │ transcdr_automation                │
            │ S3 event          │   trigger: watch | hook | queue    │
            ▼                   │   source: connection + prefix/glob │
   SQS queue or SNS topic ─(2)─▶│   preset + output overrides        │
                                │   destination: connection + prefix │
                                │                                    │
                                │ transcdr_event_destination ──(5)───┼──▶ HTTPS / SNS / SQS
                                └────────────────────────────────────┘
```

1. A **storage connection** gives Transcdr access to the bucket.
2. The **automation** learns about new files, from a queue, a hook or a listing.
3. It starts a **job** for each file, with a **preset** and optional overrides.
4. The job's outputs are delivered to a **destination** connection, often the same bucket.
5. An **event destination** is told when the job completes or fails.

The `transcdr_bucket_automation_setup` data source generates the AWS side: the IAM policy for Transcdr's user, the queue or topic policy, and the S3 notification filters. It runs inside Terraform without calling the API, and its output is identical to the dashboard's *Automate a bucket* wizard.

## Things worth knowing

- **Secrets are write-only.** Connection secrets and event destination AWS keys are never returned by the API. Terraform keeps the values from your configuration in its state and sends them again when they change. To rotate a credential, change the value and apply. The API does return a fingerprint of each secret, so a secret changed outside Terraform shows up on the next plan and is set back. Keep your state private: an encrypted remote backend, not a public repository.
- **Rotating generated secrets:**
  - A hook automation's URL rotates when you change `hook_token_version`.
  - An event destination's signing secret rotates when you change `secret_version`.
  - An API key cannot be changed: any change creates a new key.
- **Drift:** when Transcdr turns a failing connection off, the next plan shows `enabled` going back to `true`, and applying tests it and turns it back on.
- **Preset outputs are compared field by field.** Only the fields you write are compared, so API defaults are not drift. Every change updates the preset in place, and a field you remove goes back to its default. See `presets-and-destinations`.
- **Removing an optional argument clears it in place**: an automation's `destination`, `preset`, `output`, `metadata` or `webhook_url`, or an event destination's `aws.endpoint` or `aws.message_group_id`. Nothing is replaced, so an automation keeps its id, its hook URL and its record of processed files.
- **Outputs must not land in the watched prefix**, or they are picked up and transcoded again.
- **S3 suffix filters are case-sensitive.** `*.mp4` does not send `CLIP.MP4`. Transcdr's own pattern matching is case-insensitive.

## Troubleshooting

| Symptom | Look at |
|---|---|
| A connection's `status` is `error` | `terraform state show transcdr_connection.<name>` shows `last_error`. On AWS, new access keys can take a few seconds to work: the examples wait with `time_sleep`. |
| A file never becomes a job (queue) | The dead-letter queue; the bucket's notification configuration; that the file is under the prefix and has a matching suffix. `GET /v1/automations/{id}/items` lists what the automation has processed, and why a file failed. |
| A file never becomes a job (hook) | The SNS subscription's status (it should be *Confirmed*), and the topic policy. |
| A file never becomes a job (watch) | The settle time has not passed yet, or the file is outside the prefix. `POST /v1/automations/{id}/run` lists the folder at once. |
| A warning that a secret changed outside Terraform | Someone changed a connection's or event destination's secret in the dashboard or the API. The next apply sets the configured value again. |
| API errors in a plan or apply | The diagnostic shows the API's message, each field error and a request id; quote the request id to support. |

## Snippets for the docs

`provider/`, `resources/` and `data-sources/` hold the short examples, and the import commands, that appear on each page of the provider documentation. They are generated into `../docs` by `tfplugindocs`. They reference resources defined elsewhere, such as `aws_iam_access_key.transcdr`, so they are illustrations, not modules to apply.

## Tested

The acceptance tests apply `s3-sqs-queue-automation` exactly as published, against LocalStack and a Transcdr API: the whole pipeline, from bucket and queue to connections, preset, automation and SNS event destination. A `localstack_override.tf` points the AWS endpoints at LocalStack. The test uploads a file, checks that the automation turned the S3 event into a job with the pipeline's preset, changes the codec (the preset updates in place and nothing else changes), and destroys everything. Every example passes `terraform validate` and `terraform fmt -check`.
