package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccAPIKey(name string) string {
	return configHeader(false) + fmt.Sprintf(`
resource "transcdr_api_key" "test" {
  name       = %q
  scopes     = ["jobs:read", "presets:read"]
  mode       = "test"
  expires_at = "2099-01-01T00:00:00Z"
}
`, name)
}

func TestAccAPIKey(t *testing.T) {
	name := acctest.RandomWithPrefix("tfacc")
	r := "transcdr_api_key.test"
	var id string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             checkKeysRevoked,
		Steps: []resource.TestStep{
			{
				Config: testAccAPIKey(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(r, "id", regexp.MustCompile(`^key_`)),
					resource.TestMatchResourceAttr(r, "secret", regexp.MustCompile(`^tdk_test_`)),
					resource.TestMatchResourceAttr(r, "prefix", regexp.MustCompile(`^tdk_test_`)),
					resource.TestCheckResourceAttr(r, "scopes.#", "2"),
					resource.TestCheckResourceAttr(r, "mode", "test"),
					resource.TestCheckResourceAttr(r, "expires_at", "2099-01-01T00:00:00Z"),
					resource.TestCheckResourceAttrWith(r, "id", capture(&id)),
				),
			},
			{
				ResourceName:            r,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
			// Keys cannot change: a new name is a new key.
			{
				Config: testAccAPIKey(name + "-2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(r, "id", differs(&id)),
					resource.TestMatchResourceAttr(r, "secret", regexp.MustCompile(`^tdk_test_`)),
				),
			},
		},
	})
}

func checkKeysRevoked(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "transcdr_api_key" {
			continue
		}
		k, err := getAPIKey(context.Background(), testClient(), rs.Primary.ID)
		if err != nil {
			return err
		}
		if k != nil {
			return fmt.Errorf("API key %s was not revoked", rs.Primary.ID)
		}
	}
	return nil
}
