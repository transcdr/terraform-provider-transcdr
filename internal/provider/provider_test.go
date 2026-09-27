package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	transcdr "github.com/transcdr/transcdr-sdk-go"
)

// The acceptance tests run against a real Transcdr API (TRANSCDR_BASE_URL, TRANSCDR_API_KEY) and
// LocalStack for S3, SQS and SNS (TRANSCDR_LOCALSTACK_URL, default http://localhost:4566).
// scripts/testacc-up.sh starts both and writes the environment.

var testAccProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"transcdr": providerserver.NewProtocol6WithError(New("test")()),
}

// awsProvider is the AWS provider the LocalStack tests use; the harness must know it to install it.
var awsProvider = map[string]resource.ExternalProvider{
	"aws": {Source: "hashicorp/aws", VersionConstraint: "~> 6.0"},
}

func TestMain(m *testing.M) {
	// Reattach the provider under test as transcdr/transcdr, the address configs require.
	_ = os.Setenv("TF_ACC_PROVIDER_NAMESPACE", "transcdr")
	os.Exit(m.Run())
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range []string{"TRANSCDR_API_KEY", "TRANSCDR_BASE_URL"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests (see scripts/testacc-up.sh)", v)
		}
	}
}

func localstackURL() string {
	if u := os.Getenv("TRANSCDR_LOCALSTACK_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:4566"
}

// testClient talks to the API the tests run against, for checks outside Terraform.
func testClient() *transcdr.Client {
	return transcdr.NewClient(transcdr.WithUserAgent("terraform-provider-transcdr/test"))
}

// Test access keys: AWS-shaped, accepted by LocalStack.
const (
	testAccessKeyID     = "AKIAIOSFODNN7EXAMPLE"
	testSecretAccessKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
)

// configHeader requires the providers a test config uses; with aws it points AWS at LocalStack.
func configHeader(aws bool) string {
	if !aws {
		return `
terraform {
  required_providers {
    transcdr = { source = "transcdr/transcdr" }
  }
}
`
	}
	u := localstackURL()
	return fmt.Sprintf(`
terraform {
  required_providers {
    transcdr = { source = "transcdr/transcdr" }
    aws      = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region                      = "us-east-1"
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
`, u)
}

// queueURL is a LocalStack queue's URL as the local API accepts it.
func queueURL(name string) string {
	return localstackURL() + "/000000000000/" + name
}

// checkGone is a CheckDestroy that every resource of type is gone from the API.
func checkGone(resourceType, pathPrefix string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		c := testClient()
		for _, rs := range s.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			err := c.Do(context.Background(), "GET", pathPrefix+rs.Primary.ID, nil, nil)
			if err == nil {
				return fmt.Errorf("%s %s still exists", resourceType, rs.Primary.ID)
			}
			if !transcdr.IsNotFound(err) {
				return err
			}
		}
		return nil
	}
}

// capture stores an attribute's value, for comparing across steps.
func capture(target *string) func(string) error {
	return func(v string) error {
		*target = v
		return nil
	}
}

// differs checks that an attribute changed from a captured value.
func differs(before *string) func(string) error {
	return func(v string) error {
		if v == "" || v == *before {
			return fmt.Errorf("expected a new value, got %q (before: %q)", v, *before)
		}
		return nil
	}
}

// same checks that an attribute kept a captured value.
func same(before *string) func(string) error {
	return func(v string) error {
		if v != *before {
			return fmt.Errorf("expected %q to stay, got %q", *before, v)
		}
		return nil
	}
}

// checkAPI reads a resource's object straight from the API (pathPrefix + its id) and checks it.
func checkAPI(pathPrefix, address string, check func(obj map[string]any) error) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in the state", address)
		}
		var obj map[string]any
		if err := testClient().Do(context.Background(), "GET", pathPrefix+rs.Primary.ID, nil, &obj); err != nil {
			return err
		}
		return check(obj)
	}
}

// fingerprint is a write-only secret's fingerprint in an API object's `secrets`, or "".
func fingerprint(obj map[string]any, name string) string {
	secrets, _ := obj["secrets"].(map[string]any)
	s, _ := secrets[name].(map[string]any)
	fp, _ := s["fingerprint"].(string)
	return fp
}

// captureFingerprint stores a resource's secret fingerprint as the API has it.
func captureFingerprint(pathPrefix, address, name string, target *string) resource.TestCheckFunc {
	return checkAPI(pathPrefix, address, func(obj map[string]any) error {
		if *target = fingerprint(obj, name); *target == "" {
			return fmt.Errorf("%s has no fingerprint for %s", address, name)
		}
		return nil
	})
}

// sameFingerprint checks that a secret's fingerprint is the captured one: the secret is the one
// Terraform set.
func sameFingerprint(pathPrefix, address, name string, before *string) resource.TestCheckFunc {
	return checkAPI(pathPrefix, address, func(obj map[string]any) error {
		if fp := fingerprint(obj, name); fp != *before {
			return fmt.Errorf("%s: %s fingerprint %q, want %q", address, name, fp, *before)
		}
		return nil
	})
}
