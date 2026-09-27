package provider

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// examplesDir is the repository's examples/, from the package directory (where `go test` runs) or
// TRANSCDR_EXAMPLES_DIR.
func examplesDir(t *testing.T) string {
	if d := os.Getenv("TRANSCDR_EXAMPLES_DIR"); d != "" {
		return d
	}
	d, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// localstackOverride points examples/s3-sqs-queue-automation at LocalStack: the AWS provider's
// endpoints, and the endpoints Transcdr uses to reach the bucket, the queue and the topic. Terraform
// merges *_override.tf files into the module, so the rest is the example exactly as published.
func localstackOverride() string {
	u := localstackURL()
	return fmt.Sprintf(`
provider "aws" {
  access_key                  = "test"
  secret_key                  = "test"
  s3_use_path_style           = true
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  endpoints {
    iam = %[1]q
    s3  = %[1]q
    sns = %[1]q
    sqs = %[1]q
    sts = %[1]q
  }
}

resource "transcdr_connection" "bucket" {
  config = {
    bucket     = aws_s3_bucket.media.bucket
    region     = var.region
    endpoint   = %[1]q
    path_style = true
  }
}

resource "transcdr_connection" "queue" {
  config = {
    queue_url = "%[1]s/000000000000/${aws_sqs_queue.triggers.name}"
    region    = var.region
  }
}

resource "transcdr_event_destination" "completions" {
  aws = {
    access_key_id     = aws_iam_access_key.transcdr.id
    secret_access_key = aws_iam_access_key.transcdr.secret
    endpoint          = %[1]q
  }
}
`, u)
}

// TestAccExample_s3SQSQueueAutomation applies examples/s3-sqs-queue-automation against LocalStack
// and the API, uploads a video to the bucket, and proves the S3 notification reached the queue and
// the automation made a job of it: bucket -> SQS -> Transcdr -> job.
func TestAccExample_s3SQSQueueAutomation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(examplesDir(t), "s3-sqs-queue-automation")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".tf" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "localstack_override.tf"), []byte(localstackOverride()), 0o644); err != nil {
		t.Fatal(err)
	}

	name := acctest.RandomWithPrefix("tfacc-e2e")
	vars := config.Variables{
		"name":                 config.StringVariable(name),
		"iam_propagation_wait": config.StringVariable("0s"),
	}
	var presetID string
	resource.Test(t, resource.TestCase{
		PreCheck: func() { testAccPreCheck(t) },
		Steps: []resource.TestStep{
			{
				// The example has provider blocks, so the provider under test is given per step.
				ProtoV6ProviderFactories: testAccProviders,
				ConfigDirectory:          config.StaticDirectory(dir),
				ConfigVariables:          vars,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("transcdr_automation.ingest", "trigger", "queue"),
					resource.TestCheckResourceAttr("transcdr_connection.bucket", "status", "ok"),
					resource.TestCheckResourceAttr("transcdr_connection.queue", "status", "ok"),
					resource.TestCheckResourceAttr("transcdr_event_destination.completions", "type", "sns"),
					resource.TestCheckResourceAttrPair("transcdr_event_destination.completions", "target", "aws_sns_topic.events", "arn"),
					resource.TestCheckResourceAttrPair("transcdr_automation.ingest", "preset", "transcdr_preset.delivery", "id"),
					resource.TestMatchResourceAttr("transcdr_preset.delivery", "resolved_output", regexp.MustCompile(`"codec":"h264"`)),
					uploadAndExpectJob("incoming/e2e/talk.mp4"),
					resource.TestCheckResourceAttrWith("transcdr_preset.delivery", "id", capture(&presetID)),
				),
			},
			// Changing the pipeline's codec updates the preset in place; nothing else changes.
			{
				ProtoV6ProviderFactories: testAccProviders,
				ConfigDirectory:          config.StaticDirectory(dir),
				ConfigVariables:          withVar(vars, "codec", config.StringVariable("av1")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("transcdr_preset.delivery", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("transcdr_automation.ingest", plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction("transcdr_connection.bucket", plancheck.ResourceActionNoop),
						plancheck.ExpectResourceAction("transcdr_event_destination.completions", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("transcdr_preset.delivery", "id", same(&presetID)),
					resource.TestMatchResourceAttr("transcdr_preset.delivery", "resolved_output", regexp.MustCompile(`"codec":"av1"`)),
				),
			},
		},
	})
}

// uploadAndExpectJob puts a file in the example's bucket, then asks the automation to read its
// queue until the file shows up as a processed item with a job, and checks the job.
func uploadAndExpectJob(key string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		ctx := context.Background()
		bucket := s.RootModule().Resources["aws_s3_bucket.media"].Primary.Attributes["bucket"]
		automation := s.RootModule().Resources["transcdr_automation.ingest"].Primary.ID
		source := s.RootModule().Resources["transcdr_connection.bucket"].Primary.ID
		preset := s.RootModule().Resources["transcdr_preset.delivery"].Primary.ID

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/%s/%s", localstackURL(), bucket, key), bytes.NewReader([]byte("not really a video")))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "video/mp4")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("uploading to LocalStack: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("uploading to LocalStack: HTTP %d", resp.StatusCode)
		}

		c := testClient()
		deadline := time.Now().Add(90 * time.Second)
		var received int64
		for {
			run, err := c.Automations.Run(ctx, automation)
			if err != nil {
				return err
			}
			if run.MessagesReceived != nil {
				received += *run.MessagesReceived
			}
			items, err := c.Automations.Items(ctx, automation, nil)
			if err != nil {
				return err
			}
			for _, item := range items.Data {
				if item.Path != key {
					continue
				}
				if item.JobID == nil {
					return fmt.Errorf("%s was processed without a job: status %s, error %v", key, item.Status, deref(item.Error))
				}
				job, err := c.Jobs.Get(ctx, *item.JobID)
				if err != nil {
					return err
				}
				if job.Input.Type != "connection" || job.Input.ConnectionID != source || job.Input.Path != key {
					return fmt.Errorf("job %s input = %+v, want the file %s in %s", job.ID, job.Input, key, source)
				}
				if job.Metadata["automation_id"] != automation || job.Metadata["source_path"] != key {
					return fmt.Errorf("job %s metadata = %v", job.ID, job.Metadata)
				}
				if job.PresetID == nil || *job.PresetID != preset {
					return fmt.Errorf("job %s preset = %v, want %s", job.ID, deref(job.PresetID), preset)
				}
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("no job for %s after 90 s (%d queue messages read)", key, received)
			}
			time.Sleep(3 * time.Second)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// withVar is vars with one more (or a replaced) variable.
func withVar(vars config.Variables, name string, value config.Variable) config.Variables {
	out := config.Variables{name: value}
	for k, v := range vars {
		if k != name {
			out[k] = v
		}
	}
	return out
}
