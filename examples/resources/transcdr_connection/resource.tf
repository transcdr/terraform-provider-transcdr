# An S3 bucket on AWS.
resource "transcdr_connection" "ingest" {
  name = "Ingest bucket"
  kind = "s3"
  config = {
    bucket = "media-in"
    region = "us-east-1"
    root   = "videos/"
  }
  secrets = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
  }
}

# A Cloudflare R2 bucket: the S3 API with R2's endpoint and region "auto".
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

# An SQS queue that triggers queue automations.
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

# An SFTP server with a pinned host key.
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
    private_key = file("${path.module}/transcdr_ed25519")
  }
}
