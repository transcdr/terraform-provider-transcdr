// Command terraform-provider-transcdr is the Terraform provider for Transcdr.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/transcdr/terraform-provider-transcdr/internal/provider"
)

// version is set by goreleaser at release time.
var version = "dev"

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.24.0 generate --provider-name transcdr

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/transcdr/transcdr",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
