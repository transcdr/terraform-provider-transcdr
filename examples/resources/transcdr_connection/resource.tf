# An S3 bucket on AWS: where source files are read from and outputs written.
# Transcdr tests the credentials when the connection is saved; the result is
# in `status` ("ok" or "error") and `last_error`.
resource "transcdr_connection" "ingest" {
  name = "Ingest bucket"
  kind = "s3"
  config = {
    bucket = "media-in"
    region = "us-east-1"
    root   = "videos/" # optional: every path is relative to this folder
  }
  # Write-only: never returned by the API. Terraform keeps these values in
  # its state and sends them again only when they change; change them here
  # to rotate the credentials. Removing one clears it.
  secrets = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
}

# A Cloudflare R2 bucket: the S3 API with R2's endpoint and region "auto".
# Backblaze B2, Wasabi and MinIO work the same way with their own endpoint
# (MinIO also needs path_style = true).
resource "transcdr_connection" "r2" {
  name = "R2 media"
  kind = "s3"
  config = {
    bucket   = "media"
    region   = "auto"
    endpoint = "https://${var.cloudflare_account_id}.r2.cloudflarestorage.com"
  }
  secrets = {
    access_key_id     = var.r2_access_key_id
    secret_access_key = var.r2_secret_access_key
  }
}

# An SQS queue: a messaging connection that can trigger queue automations
# (and receive events). The region is read from the queue URL.
resource "transcdr_connection" "triggers" {
  name = "Upload events"
  kind = "sqs"
  config = {
    queue_url = aws_sqs_queue.triggers.url
  }
  secrets = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
}

# An SFTP server. Pinning the host key means a server presenting any other key
# is refused, instead of trusted on first use.
resource "transcdr_connection" "sftp" {
  name = "Studio SFTP"
  kind = "sftp"
  config = {
    host                 = "sftp.example.com"
    username             = "transcdr"
    root                 = "/exports"
    host_key_fingerprint = "SHA256:zE6s2Wf0gXJt0h9Q2mT7r8x1c4V5b6N7m8K9l0P1q2w"
  }
  secrets = {
    # Or password = "…"; an encrypted key also needs private_key_passphrase.
    private_key = file("${path.module}/transcdr_ed25519")
  }
}
