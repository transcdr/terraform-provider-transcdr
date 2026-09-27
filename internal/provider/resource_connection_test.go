package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

func testAccS3ConnectionConfig(bucket, name, root string, enabled bool, secret string) string {
	return configHeader(true) + fmt.Sprintf(`
resource "aws_s3_bucket" "b" {
  bucket        = %[1]q
  force_destroy = true
}

resource "transcdr_connection" "test" {
  name    = %[2]q
  kind    = "s3"
  enabled = %[4]t
  config = {
    bucket     = aws_s3_bucket.b.bucket
    region     = "us-east-1"
    endpoint   = %[6]q
    path_style = true
    root       = %[3]q
  }
  secrets = {
    access_key_id     = %[7]q
    secret_access_key = %[5]q
  }
}
`, bucket, name, root, enabled, secret, localstackURL(), testAccessKeyID)
}

func TestAccConnection_s3(t *testing.T) {
	bucket := acctest.RandomWithPrefix("tfacc")
	name := "tfacc " + bucket
	r := "transcdr_connection.test"
	var id, fp string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		ExternalProviders:        awsProvider,
		CheckDestroy:             checkGone("transcdr_connection", "/v1/connections/"),
		Steps: []resource.TestStep{
			{
				Config: testAccS3ConnectionConfig(bucket, name, "videos/", true, testSecretAccessKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "id", regexp.MustCompile(`^con_`)),
					resource.TestCheckResourceAttr(r, "kind", "s3"),
					resource.TestCheckResourceAttr(r, "class", "storage"),
					resource.TestCheckResourceAttr(r, "status", "ok"),
					resource.TestCheckResourceAttr(r, "enabled", "true"),
					resource.TestCheckResourceAttr(r, "capabilities.source", "true"),
					resource.TestCheckResourceAttr(r, "capabilities.destination", "true"),
					resource.TestCheckResourceAttr(r, "capabilities.watch", "true"),
					resource.TestCheckResourceAttr(r, "capabilities.trigger", "false"),
					resource.TestCheckResourceAttr(r, "config.root", "videos/"),
					resource.TestCheckResourceAttr(r, "secrets_set.#", "2"),
					resource.TestCheckTypeSetElemAttr(r, "secrets_set.*", "secret_access_key"),
					resource.TestCheckResourceAttr(r, "failure_count", "0"),
				),
			},
			// Import: everything but the write-only secrets comes back.
			{
				ResourceName:            r,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secrets"},
			},
			// Update in place: name, a config field removed (root), turned off by hand.
			{
				Config: testAccS3ConnectionConfig(bucket, name+" renamed", "", false, testSecretAccessKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "name", name+" renamed"),
					resource.TestCheckResourceAttr(r, "config.root", ""),
					resource.TestCheckResourceAttr(r, "enabled", "false"),
					resource.TestCheckResourceAttr(r, "disabled_reason", "Disabled by hand."),
				),
			},
			// Rotating a secret: a changed value is sent; turning it back on retests it.
			{
				Config: testAccS3ConnectionConfig(bucket, name, "", true, testSecretAccessKey+"2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "enabled", "true"),
					resource.TestCheckResourceAttr(r, "secrets.secret_access_key", testSecretAccessKey+"2"),
					resource.TestCheckNoResourceAttr(r, "disabled_reason"),
					resource.TestCheckResourceAttrWith(r, "id", capture(&id)),
					captureFingerprint("/v1/connections/", r, "secret_access_key", &fp),
				),
			},
			// Drift: a secret replaced outside Terraform. Its fingerprint changes, so the plan sends
			// the configured value again, and applying restores it (the same fingerprint as before).
			{
				PreConfig: func() {
					_, err := testClient().Connections.Update(context.Background(), id, &transcdr.ConnectionUpdateParams{
						Secrets: &transcdr.ConnectionSecrets{SecretAccessKey: transcdr.String(testSecretAccessKey + "-elsewhere")},
					})
					if err != nil {
						t.Fatal(err)
					}
				},
				Config:             testAccS3ConnectionConfig(bucket, name, "", true, testSecretAccessKey+"2"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccS3ConnectionConfig(bucket, name, "", true, testSecretAccessKey+"2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "secrets.secret_access_key", testSecretAccessKey+"2"),
					sameFingerprint("/v1/connections/", r, "secret_access_key", &fp),
				),
			},
			// Drift: turned off outside Terraform, the plan turns it back on.
			{
				PreConfig: func() {
					id := stateID(t, "transcdr_connection", bucket)
					if _, err := testClient().Connections.Disable(context.Background(), id); err != nil {
						t.Fatal(err)
					}
				},
				Config:             testAccS3ConnectionConfig(bucket, name, "", true, testSecretAccessKey+"2"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: testAccS3ConnectionConfig(bucket, name, "", true, testSecretAccessKey+"2"),
				Check:  resource.TestCheckResourceAttr(r, "enabled", "true"),
			},
		},
	})
}

func testAccMessagingConnectionsConfig(suffix, webhookURL string) string {
	return configHeader(true) + fmt.Sprintf(`
resource "aws_sqs_queue" "q" {
  name = %[1]q
}

resource "aws_sns_topic" "t" {
  name = %[1]q
}

resource "transcdr_connection" "sqs" {
  name = "tfacc sqs %[1]s"
  kind = "sqs"
  config = {
    queue_url = %[3]q
    region    = "us-east-1"
  }
  secrets = {
    access_key_id     = %[5]q
    secret_access_key = %[6]q
  }
  depends_on = [aws_sqs_queue.q]
}

resource "transcdr_connection" "sns" {
  name = "tfacc sns %[1]s"
  kind = "sns"
  config = {
    topic_arn = aws_sns_topic.t.arn
    endpoint  = %[4]q
  }
  secrets = {
    access_key_id     = %[5]q
    secret_access_key = %[6]q
  }
}

resource "transcdr_connection" "webhook" {
  name = "tfacc webhook %[1]s"
  kind = "webhook"
  config = {
    url = %[2]q
  }
}
`, suffix, webhookURL, queueURL(suffix), localstackURL(), testAccessKeyID, testSecretAccessKey)
}

func TestAccConnection_messaging(t *testing.T) {
	suffix := acctest.RandomWithPrefix("tfacc")
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		ExternalProviders:        awsProvider,
		CheckDestroy:             checkGone("transcdr_connection", "/v1/connections/"),
		Steps: []resource.TestStep{
			{
				Config: testAccMessagingConnectionsConfig(suffix, "https://example.com/hooks/transcdr"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("transcdr_connection.sqs", "class", "messaging"),
					resource.TestCheckResourceAttr("transcdr_connection.sqs", "capabilities.trigger", "true"),
					resource.TestCheckResourceAttr("transcdr_connection.sqs", "capabilities.events", "true"),
					resource.TestCheckResourceAttr("transcdr_connection.sqs", "capabilities.source", "false"),
					resource.TestCheckResourceAttr("transcdr_connection.sqs", "status", "ok"),
					resource.TestCheckResourceAttr("transcdr_connection.sns", "class", "messaging"),
					resource.TestCheckResourceAttr("transcdr_connection.sns", "capabilities.trigger", "false"),
					// The region is read from the topic ARN, so it is not configuration.
					resource.TestCheckNoResourceAttr("transcdr_connection.sns", "config.region"),
					resource.TestCheckResourceAttr("transcdr_connection.webhook", "class", "messaging"),
					resource.TestCheckResourceAttr("transcdr_connection.webhook", "secrets_set.#", "0"),
				),
			},
			{
				ResourceName:            "transcdr_connection.sqs",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secrets"},
			},
			{
				ResourceName:      "transcdr_connection.sns",
				ImportState:       true,
				ImportStateVerify: true,
				// The API stores URLs normalised (http://localhost:4566/); an import has no other spelling.
				ImportStateVerifyIgnore: []string{"secrets", "config.endpoint"},
			},
			{
				ResourceName:      "transcdr_connection.webhook",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccMessagingConnectionsConfig(suffix, "https://example.com/hooks/transcdr-v2"),
				Check:  resource.TestCheckResourceAttr("transcdr_connection.webhook", "config.url", "https://example.com/hooks/transcdr-v2"),
			},
		},
	})
}

func TestAccConnection_invalid(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				// The API's field error is attached to the attribute, with its request id.
				Config: configHeader(false) + `
resource "transcdr_connection" "bad" {
  name   = "tfacc bad"
  kind   = "sqs"
  config = { queue_url = "not a url" }
  secrets = {
    access_key_id     = "AKIAIOSFODNN7EXAMPLE"
    secret_access_key = "x"
  }
}
`,
				ExpectError: regexp.MustCompile(`(?s)Could not create the connection.*config\.queue_url.*Request\s+id`),
			},
		},
	})
}

// stateID finds, through the API, the id of the connection or other object whose name contains
// marker. PreConfig runs before the step's state is available, so tests look objects up by name.
func stateID(t *testing.T, resourceType, marker string) string {
	t.Helper()
	paths := map[string]string{
		"transcdr_connection": "/v1/connections",
		"transcdr_automation": "/v1/automations",
		"transcdr_preset":     "/v1/presets",
	}
	var list struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := testClient().Do(context.Background(), "GET", paths[resourceType], nil, &list); err != nil {
		t.Fatal(err)
	}
	for _, o := range list.Data {
		if regexp.MustCompile(regexp.QuoteMeta(marker)).MatchString(o.Name) {
			return o.ID
		}
	}
	t.Fatalf("no %s named like %s", resourceType, marker)
	return ""
}
