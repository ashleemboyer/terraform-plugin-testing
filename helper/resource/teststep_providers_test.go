// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-cty/cty"
	fwdiag "github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwresourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/customdiff"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/hashicorp/terraform-plugin-testing/internal/plugintest"
	"github.com/hashicorp/terraform-plugin-testing/internal/testing/testprovider"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestStepConfigHasProviderBlock(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		testStep TestStep
		expected bool
	}{
		"no-config": {
			testStep: TestStep{},
			expected: false,
		},
		"provider-meta-attribute": {
			testStep: TestStep{
				Config: `
resource "test_test" "test" {
  provider = test.test
}
`,
			},
			expected: false,
		},
		"provider-object-attribute": {
			testStep: TestStep{
				Config: `
resource "test_test" "test" {
  test = {
	provider = {
	  test = true
	}
  }
}
`,
			},
			expected: false,
		},
		"provider-string-attribute": {
			testStep: TestStep{
				Config: `
resource "test_test" "test" {
  test = {
	provider = "test"
  }
}
`,
			},
			expected: false,
		},
		"provider-block-quoted-with-attributes": {
			testStep: TestStep{
				Config: `
provider "test" {
  test = true
}

resource "test_test" "test" {}
`,
			},
			expected: true,
		},
		"provider-block-unquoted-with-attributes": {
			testStep: TestStep{
				Config: `
provider test {
  test = true
}

resource "test_test" "test" {}
`,
			},
			expected: true,
		},
		"provider-block-quoted-without-attributes": {
			testStep: TestStep{
				Config: `
provider "test" {}

resource "test_test" "test" {}
`,
			},
			expected: true,
		},
		"provider-block-unquoted-without-attributes": {
			testStep: TestStep{
				Config: `
provider test {}

resource "test_test" "test" {}
`,
			},
			expected: true,
		},
	}

	for name, testCase := range testCases {
		name, testCase := name, testCase

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := testCase.testStep.configHasProviderBlock(context.Background())

			if testCase.expected != got {
				t.Errorf("expected %t, got %t", testCase.expected, got)
			}
		})
	}
}

func TestStepMergedConfig(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		testCase TestCase
		testStep TestStep
		expected string
	}{
		"testcase-externalproviders-and-protov5providerfactories": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"localtest": nil,
				},
			},
			testStep: TestStep{
				Config: `
resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}


resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
		},
		"testcase-externalproviders-and-protov6providerfactories": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"localtest": nil,
				},
			},
			testStep: TestStep{
				Config: `
resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}


resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
		},
		"testcase-externalproviders-and-providerfactories": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"localtest": nil,
				},
			},
			testStep: TestStep{
				Config: `
resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}


resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
		},
		"testcase-externalproviders-missing-source-and-versionconstraint": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"test": {},
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
provider "test" {}

resource "test_test" "test" {}
`,
		},
		"testcase-externalproviders-source-and-versionconstraint": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}

provider "test" {}


resource "test_test" "test" {}
`,
		},
		"testcase-externalproviders-source": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source: "registry.terraform.io/hashicorp/test",
					},
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
    }
  }
}

provider "test" {}


resource "test_test" "test" {}
`,
		},
		"testcase-externalproviders-versionconstraint": {
			testCase: TestCase{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						VersionConstraint: "1.2.3",
					},
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      version = "1.2.3"
    }
  }
}

provider "test" {}


resource "test_test" "test" {}
`,
		},
		"testcase-protov5providerfactories": {
			testCase: TestCase{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"test": nil,
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
resource "test_test" "test" {}
`,
		},
		"testcase-protov6providerfactories": {
			testCase: TestCase{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"test": nil,
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
resource "test_test" "test" {}
`,
		},
		"testcase-providerfactories": {
			testCase: TestCase{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"test": nil,
				},
			},
			testStep: TestStep{
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-and-protov5providerfactories": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"localtest": nil,
				},
				Config: `
resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}


resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
		},
		"teststep-externalproviders-and-protov6providerfactories": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"localtest": nil,
				},
				Config: `
resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}


resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
		},
		"teststep-externalproviders-and-providerfactories": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"localtest": nil,
				},
				Config: `
resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}


resource "externaltest_test" "test" {}

resource "localtest_test" "test" {}
`,
		},
		"teststep-externalproviders-config-with-provider-block-quoted": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
				Config: `
provider "test" {}

resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}



provider "test" {}

resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-config-with-provider-block-unquoted": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
				Config: `
provider test {}

resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}



provider test {}

resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-config-with-terraform-block": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
				Config: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}

resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}

resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-missing-source-and-versionconstraint": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {},
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
provider "test" {}

resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-source-and-versionconstraint": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}

provider "test" {}


resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-source": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source: "registry.terraform.io/hashicorp/test",
					},
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
    }
  }
}

provider "test" {}


resource "test_test" "test" {}
`,
		},
		"teststep-externalproviders-versionconstraint": {
			testCase: TestCase{},
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						VersionConstraint: "1.2.3",
					},
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
terraform {
  required_providers {
    test = {
      version = "1.2.3"
    }
  }
}

provider "test" {}


resource "test_test" "test" {}
`,
		},
		"teststep-protov5providerfactories": {
			testCase: TestCase{},
			testStep: TestStep{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"test": nil,
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
resource "test_test" "test" {}
`,
		},
		"teststep-protov6providerfactories": {
			testCase: TestCase{},
			testStep: TestStep{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"test": nil,
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
resource "test_test" "test" {}
`,
		},
		"teststep-providerfactories": {
			testCase: TestCase{},
			testStep: TestStep{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"test": nil,
				},
				Config: `
resource "test_test" "test" {}
`,
			},
			expected: `
resource "test_test" "test" {}
`,
		},
	}

	for name, testCase := range testCases {
		name, testCase := name, testCase

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := testCase.testStep.mergedConfig(context.Background(), testCase.testCase)

			if diff := cmp.Diff(strings.TrimSpace(got), strings.TrimSpace(testCase.expected)); diff != "" {
				t.Errorf("unexpected difference: %s", diff)
			}
		})
	}
}

func TestStepProviderConfig(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		testStep          TestStep
		skipProviderBlock bool
		expected          string
	}{
		"externalproviders-and-protov5providerfactories": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"localtest": nil,
				},
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}
`,
		},
		"externalproviders-and-protov6providerfactories": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"localtest": nil,
				},
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}
`,
		},
		"externalproviders-and-providerfactories": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"externaltest": {
						Source:            "registry.terraform.io/hashicorp/externaltest",
						VersionConstraint: "1.2.3",
					},
				},
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"localtest": nil,
				},
			},
			expected: `
terraform {
  required_providers {
    externaltest = {
      source = "registry.terraform.io/hashicorp/externaltest"
      version = "1.2.3"
    }
  }
}

provider "externaltest" {}
`,
		},
		"externalproviders-missing-source-and-versionconstraint": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {},
				},
			},
			expected: `provider "test" {}`,
		},
		"externalproviders-skip-provider-block": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
			},
			skipProviderBlock: true,
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}
`,
		},
		"externalproviders-source-and-versionconstraint": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source:            "registry.terraform.io/hashicorp/test",
						VersionConstraint: "1.2.3",
					},
				},
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
      version = "1.2.3"
    }
  }
}

provider "test" {}
`,
		},
		"externalproviders-source": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						Source: "registry.terraform.io/hashicorp/test",
					},
				},
			},
			expected: `
terraform {
  required_providers {
    test = {
      source = "registry.terraform.io/hashicorp/test"
    }
  }
}

provider "test" {}
`,
		},
		"externalproviders-versionconstraint": {
			testStep: TestStep{
				ExternalProviders: map[string]ExternalProvider{
					"test": {
						VersionConstraint: "1.2.3",
					},
				},
			},
			expected: `
terraform {
  required_providers {
    test = {
      version = "1.2.3"
    }
  }
}

provider "test" {}
`,
		},
		"protov5providerfactories": {
			testStep: TestStep{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"test": nil,
				},
			},
			expected: ``,
		},
		"protov6providerfactories": {
			testStep: TestStep{
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"test": nil,
				},
			},
			expected: ``,
		},
		"providerfactories": {
			testStep: TestStep{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"test": nil,
				},
			},
			expected: ``,
		},
	}

	for name, testCase := range testCases {
		name, testCase := name, testCase

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := testCase.testStep.providerConfig(context.Background(), testCase.skipProviderBlock)

			if diff := cmp.Diff(strings.TrimSpace(got), strings.TrimSpace(testCase.expected)); diff != "" {
				t.Errorf("unexpected difference: %s", diff)
			}
		})
	}
}

func TestTest_TestStep_ExternalProviders(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: "# not empty",
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source: "registry.terraform.io/hashicorp/null",
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ExternalProviders_DifferentProviders(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: `resource "null_resource" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source: "registry.terraform.io/hashicorp/null",
					},
				},
			},
			{
				Config: `resource "random_pet" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"random": {
						Source: "registry.terraform.io/hashicorp/random",
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ExternalProviders_DifferentVersions(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: `resource "null_resource" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source:            "registry.terraform.io/hashicorp/null",
						VersionConstraint: "3.1.0",
					},
				},
			},
			{
				Config: `resource "null_resource" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source:            "registry.terraform.io/hashicorp/null",
						VersionConstraint: "3.1.1",
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ExternalProviders_Error(t *testing.T) {
	t.Parallel()

	testExpectTFatal(t, func() {
		Test(&mockT{}, TestCase{
			Steps: []TestStep{
				{
					Config: "# not empty",
					ExternalProviders: map[string]ExternalProvider{
						"testnonexistent": {
							Source: "registry.terraform.io/hashicorp/testnonexistent",
						},
					},
				},
			},
		})
	})
}

func TestTest_TestStep_ExternalProviders_NonHashiCorpNamespace(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ExternalProviders: map[string]ExternalProvider{
					// This can be set to any provider outside the hashicorp namespace.
					// bflad/scaffoldingtest happens to be a published version of
					// terraform-provider-scaffolding-framework.
					"scaffoldingtest": {
						Source:            "registry.terraform.io/bflad/scaffoldingtest",
						VersionConstraint: "0.1.0",
					},
				},
				Config: `resource "scaffoldingtest_example" "test" {}`,
			},
		},
	})
}

func TestTest_TestStep_ExternalProvidersAndProviderFactories_NonHashiCorpNamespace(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ExternalProviders: map[string]ExternalProvider{
					// This can be set to any provider outside the hashicorp namespace.
					// bflad/scaffoldingtest happens to be a published version of
					// terraform-provider-scaffolding-framework.
					"scaffoldingtest": {
						Source:            "registry.terraform.io/bflad/scaffoldingtest",
						VersionConstraint: "0.1.0",
					},
				},
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"null": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"null_resource": {
									CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
										d.SetId("test")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"triggers": {
											Elem:     &schema.Schema{Type: schema.TypeString},
											ForceNew: true,
											Optional: true,
											Type:     schema.TypeMap,
										},
									},
								},
							},
						}, nil
					},
				},
				Config: `
					resource "null_resource" "test" {}
					resource "scaffoldingtest_example" "test" {}
				`,
			},
		},
	})
}

func TestTest_TestStep_ExternalProviders_To_ProviderFactories(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: `resource "null_resource" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source:            "registry.terraform.io/hashicorp/null",
						VersionConstraint: "3.1.1",
					},
				},
			},
			{
				Config: `resource "null_resource" "test" {}`,
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"null": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"null_resource": {
									CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
										d.SetId("test")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"triggers": {
											Elem:     &schema.Schema{Type: schema.TypeString},
											ForceNew: true,
											Optional: true,
											Type:     schema.TypeMap,
										},
									},
								},
							},
						}, nil
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ExternalProviders_To_ProviderFactories_StateUpgraders(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: `resource "null_resource" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source:            "registry.terraform.io/hashicorp/null",
						VersionConstraint: "3.1.1",
					},
				},
			},
			{
				Check:  TestCheckResourceAttr("null_resource.test", "id", "test-schema-version-1"),
				Config: `resource "null_resource" "test" {}`,
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"null": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"null_resource": {
									CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
										d.SetId("test")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"triggers": {
											Elem:     &schema.Schema{Type: schema.TypeString},
											ForceNew: true,
											Optional: true,
											Type:     schema.TypeMap,
										},
									},
									SchemaVersion: 1, // null 3.1.3 is version 0
									StateUpgraders: []schema.StateUpgrader{
										{
											Type: cty.Object(map[string]cty.Type{
												"id":       cty.String,
												"triggers": cty.Map(cty.String),
											}),
											Upgrade: func(ctx context.Context, rawState map[string]interface{}, meta interface{}) (map[string]interface{}, error) {
												// null 3.1.3 sets the id attribute to a stringified random integer.
												// Double check that our resource wasn't created by this TestStep.
												id, ok := rawState["id"].(string)

												if !ok || id == "test" {
													return rawState, fmt.Errorf("unexpected rawState: %v", rawState)
												}

												rawState["id"] = "test-schema-version-1"

												return rawState, nil
											},
											Version: 0,
										},
									},
								},
							},
						}, nil
					},
				},
			},
		},
	})
}

func TestTest_TestStep_Taint(t *testing.T) {
	t.Parallel()

	var idOne, idTwo string

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_id": {
							CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								d.SetId(time.Now().String())
								return nil
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config: `resource "random_id" "test" {}`,
				Check: ComposeAggregateTestCheckFunc(
					extractResourceAttr("random_id.test", "id", &idOne),
				),
			},
			{
				Taint:  []string{"random_id.test"},
				Config: `resource "random_id" "test" {}`,
				Check: ComposeAggregateTestCheckFunc(
					extractResourceAttr("random_id.test", "id", &idTwo),
				),
			},
		},
	})

	if idOne == idTwo {
		t.Errorf("taint is not causing destroy-create cycle, idOne == idTwo: %s == %s", idOne, idTwo)
	}
}

//nolint:unparam
func extractResourceAttr(resourceName string, attributeName string, attributeValue *string) TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]

		if !ok {
			return fmt.Errorf("resource name %s not found in state", resourceName)
		}

		attrValue, ok := rs.Primary.Attributes[attributeName]

		if !ok {
			return fmt.Errorf("attribute %s not found in resource %s state", attributeName, resourceName)
		}

		*attributeValue = attrValue

		return nil
	}
}

func TestTest_TestStep_ProtoV5ProviderFactories(t *testing.T) {
	t.Parallel()

	Test(&mockT{}, TestCase{
		Steps: []TestStep{
			{
				Config: "# not empty",
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"test": func() (tfprotov5.ProviderServer, error) { //nolint:unparam // required signature
						return nil, nil
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ProtoV5ProviderFactories_Error(t *testing.T) {
	t.Parallel()

	testExpectTFatal(t, func() {
		Test(&mockT{}, TestCase{
			Steps: []TestStep{
				{
					Config: "# not empty",
					ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
						"test": func() (tfprotov5.ProviderServer, error) { //nolint:unparam // required signature
							return nil, fmt.Errorf("test")
						},
					},
				},
			},
		})
	})
}

func TestTest_TestStep_ProtoV6ProviderFactories(t *testing.T) {
	t.Parallel()

	Test(&mockT{}, TestCase{
		Steps: []TestStep{
			{
				Config: "# not empty",
				ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
					"test": func() (tfprotov6.ProviderServer, error) { //nolint:unparam // required signature
						return nil, nil
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ProtoV6ProviderFactories_Error(t *testing.T) {
	t.Parallel()

	testExpectTFatal(t, func() {
		Test(&mockT{}, TestCase{
			Steps: []TestStep{
				{
					Config: "# not empty",
					ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
						"test": func() (tfprotov6.ProviderServer, error) { //nolint:unparam // required signature
							return nil, fmt.Errorf("test")
						},
					},
				},
			},
		})
	})
}

func TestTest_TestStep_ProviderFactories(t *testing.T) {
	t.Parallel()

	Test(&mockT{}, TestCase{
		Steps: []TestStep{
			{
				Config: "# not empty",
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"test": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return nil, nil
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Error(t *testing.T) {
	t.Parallel()

	testExpectTFatal(t, func() {
		Test(&mockT{}, TestCase{
			Steps: []TestStep{
				{
					Config: "# not empty",
					ProviderFactories: map[string]func() (*schema.Provider, error){
						"test": func() (*schema.Provider, error) { //nolint:unparam // required signature
							return nil, fmt.Errorf("test")
						},
					},
				},
			},
		})
	})
}

func TestTest_TestStep_ProviderFactories_To_ExternalProviders(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: `resource "null_resource" "test" {}`,
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"null": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"null_resource": {
									CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
										d.SetId("test")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"triggers": {
											Elem:     &schema.Schema{Type: schema.TypeString},
											ForceNew: true,
											Optional: true,
											Type:     schema.TypeMap,
										},
									},
								},
							},
						}, nil
					},
				},
			},
			{
				Config: `resource "null_resource" "test" {}`,
				ExternalProviders: map[string]ExternalProvider{
					"null": {
						Source: "registry.terraform.io/hashicorp/null",
					},
				},
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_Inline(t *testing.T) {
	id := "none"

	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				Config: `resource "random_password" "test" { length = 12 }`,
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"random_password": {
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"length": {
											Required: true,
											ForceNew: true,
											Type:     schema.TypeInt,
										},
										"result": {
											Type:      schema.TypeString,
											Computed:  true,
											Sensitive: true,
										},

										"id": {
											Computed: true,
											Type:     schema.TypeString,
										},
									},
									Importer: &schema.ResourceImporter{
										StateContext: func(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
											val := d.Id()

											d.SetId("none")

											err := d.Set("result", val)
											if err != nil {
												panic(err)
											}

											err = d.Set("length", len(val))
											if err != nil {
												panic(err)
											}

											return []*schema.ResourceData{d}, nil
										},
									},
								},
							},
						}, nil
					},
				},
				ResourceName:       "random_password.test",
				ImportState:        true,
				ImportStateId:      "Z=:cbrJE?Ltg",
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateCheck(
					testCheckResourceAttrInstanceState(&id, "result", "Z=:cbrJE?Ltg"),
					testCheckResourceAttrInstanceState(&id, "length", "12"),
				),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_Inline_WithPersistMatch(t *testing.T) {
	var result1, result2 string

	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"length": {
									Required: true,
									ForceNew: true,
									Type:     schema.TypeInt,
								},
								"result": {
									Type:      schema.TypeString,
									Computed:  true,
									Sensitive: true,
								},

								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
							Importer: &schema.ResourceImporter{
								StateContext: func(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
									val := d.Id()

									d.SetId("none")

									err := d.Set("result", val)
									if err != nil {
										panic(err)
									}

									err = d.Set("length", len(val))
									if err != nil {
										panic(err)
									}

									return []*schema.ResourceData{d}, nil
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config:             `resource "random_password" "test" { length = 12 }`,
				ResourceName:       "random_password.test",
				ImportState:        true,
				ImportStateId:      "Z=:cbrJE?Ltg",
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateCheck(
					testExtractResourceAttrInstanceState("none", "result", &result1),
				),
			},
			{
				Config: `resource "random_password" "test" { length = 12 }`,
				Check: ComposeTestCheckFunc(
					testExtractResourceAttr("random_password.test", "result", &result2),
					testCheckAttributeValuesEqual(&result1, &result2),
				),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_Inline_WithoutPersist(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								d.SetId("none")
								return nil
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"length": {
									Required: true,
									ForceNew: true,
									Type:     schema.TypeInt,
								},
								"result": {
									Type:      schema.TypeString,
									Computed:  true,
									Sensitive: true,
								},

								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
							Importer: &schema.ResourceImporter{
								StateContext: func(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
									val := d.Id()

									d.SetId("none")

									err := d.Set("result", val)
									if err != nil {
										panic(err)
									}

									err = d.Set("length", len(val))
									if err != nil {
										panic(err)
									}

									return []*schema.ResourceData{d}, nil
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config:             `resource "random_password" "test" { length = 12 }`,
				ResourceName:       "random_password.test",
				ImportState:        true,
				ImportStateId:      "Z=:cbrJE?Ltg",
				ImportStatePersist: false,
			},
			{
				Config: `resource "random_password" "test" { length = 12 }`,
				Check: ComposeTestCheckFunc(
					TestCheckNoResourceAttr("random_password.test", "result"),
				),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_External(t *testing.T) {
	id := "none"

	t.Parallel()

	Test(t, TestCase{
		ExternalProviders: map[string]ExternalProvider{
			"random": {
				Source: "registry.terraform.io/hashicorp/random",
			},
		},
		Steps: []TestStep{
			{
				Config:             `resource "random_password" "test" { length = 12 }`,
				ResourceName:       "random_password.test",
				ImportState:        true,
				ImportStateId:      "Z=:cbrJE?Ltg",
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateCheck(
					testCheckResourceAttrInstanceState(&id, "result", "Z=:cbrJE?Ltg"),
					testCheckResourceAttrInstanceState(&id, "length", "12"),
				),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_External_WithPersistMatch(t *testing.T) {
	var result1, result2 string

	t.Parallel()

	Test(t, TestCase{
		ExternalProviders: map[string]ExternalProvider{
			"random": {
				Source: "registry.terraform.io/hashicorp/random",
			},
		},
		Steps: []TestStep{
			{
				Config:             `resource "random_password" "test" { length = 12 }`,
				ResourceName:       "random_password.test",
				ImportState:        true,
				ImportStateId:      "Z=:cbrJE?Ltg",
				ImportStatePersist: true,
				ImportStateCheck: composeImportStateCheck(
					testExtractResourceAttrInstanceState("none", "result", &result1),
				),
			},
			{
				Config: `resource "random_password" "test" { length = 12 }`,
				Check: ComposeTestCheckFunc(
					testExtractResourceAttr("random_password.test", "result", &result2),
					testCheckAttributeValuesEqual(&result1, &result2),
				),
			},
		},
	})
}

//nolint:paralleltest // Can't use t.Parallel with t.Setenv
func TestTest_TestStep_ProviderFactories_Import_External_WithPersistMatch_WithPersistWorkingDir(t *testing.T) {
	var result1, result2 string

	t.Setenv(plugintest.EnvTfAccPersistWorkingDir, "1")
	workingDir := t.TempDir()

	testSteps := []TestStep{
		{
			Config:             `resource "random_password" "test" { length = 12 }`,
			ResourceName:       "random_password.test",
			ImportState:        true,
			ImportStateId:      "Z=:cbrJE?Ltg",
			ImportStatePersist: true,
			ImportStateCheck: composeImportStateCheck(
				testExtractResourceAttrInstanceState("none", "result", &result1),
			),
		},
		{
			Config: `resource "random_password" "test" { length = 12 }`,
			Check: ComposeTestCheckFunc(
				testExtractResourceAttr("random_password.test", "result", &result2),
				testCheckAttributeValuesEqual(&result1, &result2),
			),
		},
	}

	Test(t, TestCase{
		ExternalProviders: map[string]ExternalProvider{
			"random": {
				Source: "registry.terraform.io/hashicorp/random",
			},
		},
		WorkingDir: workingDir,
		Steps:      testSteps,
	})

	workingDirPath := filepath.Dir(workingDir)

	for testStepIndex := range testSteps {
		dir := workingDirPath + "_" + strconv.Itoa(testStepIndex+1)

		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("cannot read dir: %s", dir)
		}

		var workingDirName string

		// Relies upon convention of a directory being created that is prefixed "work".
		for _, dirEntry := range dirEntries {
			if strings.HasPrefix(dirEntry.Name(), "work") && dirEntry.IsDir() {
				workingDirName = filepath.Join(dir, dirEntry.Name())
				break
			}
		}

		configPlanStateFiles := []string{
			"terraform_plugin_test.tf",
			"terraform.tfstate",
			"tfplan",
		}

		for _, file := range configPlanStateFiles {
			// Skip verifying plan for first test step as there is no plan file if the
			// resource does not already exist.
			if testStepIndex == 0 && file == "tfplan" {
				break
			}
			_, err = os.Stat(filepath.Join(workingDirName, file))
			if err != nil {
				t.Errorf("cannot stat %s in %s: %s", file, workingDirName, err)
			}
		}
	}
}

func TestTest_TestStep_ProviderFactories_Import_External_WithoutPersistNonMatch(t *testing.T) {
	var result1, result2 string

	t.Parallel()

	Test(t, TestCase{
		ExternalProviders: map[string]ExternalProvider{
			"random": {
				Source: "registry.terraform.io/hashicorp/random",
			},
		},
		Steps: []TestStep{
			{
				Config:             `resource "random_password" "test" { length = 12 }`,
				ResourceName:       "random_password.test",
				ImportState:        true,
				ImportStateId:      "Z=:cbrJE?Ltg",
				ImportStatePersist: false,
				ImportStateCheck: composeImportStateCheck(
					testExtractResourceAttrInstanceState("none", "result", &result1),
				),
			},
			{
				Config: `resource "random_password" "test" { length = 12 }`,
				Check: ComposeTestCheckFunc(
					testExtractResourceAttr("random_password.test", "result", &result2),
					testCheckAttributeValuesDiffer(&result1, &result2),
				),
			},
		},
	})
}

//nolint:paralleltest // Can't use t.Parallel with t.Setenv
func TestTest_TestStep_ProviderFactories_Import_External_WithoutPersistNonMatch_WithPersistWorkingDir(t *testing.T) {
	var result1, result2 string

	t.Setenv(plugintest.EnvTfAccPersistWorkingDir, "1")
	workingDir := t.TempDir()

	testSteps := []TestStep{
		{
			Config:             `resource "random_password" "test" { length = 12 }`,
			ResourceName:       "random_password.test",
			ImportState:        true,
			ImportStateId:      "Z=:cbrJE?Ltg",
			ImportStatePersist: false,
			ImportStateCheck: composeImportStateCheck(
				testExtractResourceAttrInstanceState("none", "result", &result1),
			),
		},
		{
			Config: `resource "random_password" "test" { length = 12 }`,
			Check: ComposeTestCheckFunc(
				testExtractResourceAttr("random_password.test", "result", &result2),
				testCheckAttributeValuesDiffer(&result1, &result2),
			),
		},
	}

	Test(t, TestCase{
		ExternalProviders: map[string]ExternalProvider{
			"random": {
				Source: "registry.terraform.io/hashicorp/random",
			},
		},
		WorkingDir: workingDir,
		Steps:      testSteps,
	})

	workingDirPath := filepath.Dir(workingDir)

	for testStepIndex := range testSteps {
		dir := workingDirPath + "_" + strconv.Itoa(testStepIndex+1)

		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("cannot read dir: %s", dir)
		}

		var workingDirName string

		// Relies upon convention of a directory being created that is prefixed "work".
		for _, dirEntry := range dirEntries {
			if strings.HasPrefix(dirEntry.Name(), "work") && dirEntry.IsDir() {
				workingDirName = filepath.Join(dir, dirEntry.Name())
				break
			}
		}

		configPlanStateFiles := []string{
			"terraform_plugin_test.tf",
			"terraform.tfstate",
			"tfplan",
		}

		for _, file := range configPlanStateFiles {
			// Skip verifying state and plan for first test step as ImportStatePersist is
			// false so the state is not persisted and there is no plan file if the
			// resource does not already exist.
			if testStepIndex == 0 && (file == "terraform.tfstate" || file == "tfplan") {
				break
			}
			_, err = os.Stat(filepath.Join(workingDirName, file))
			if err != nil {
				t.Errorf("cannot stat %s in %s: %s", file, workingDirName, err)
			}
		}
	}
}

func TestTest_TestStep_ProviderFactories_Refresh_Inline(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
								d.SetId("id")
								err := d.Set("min_special", 10)
								if err != nil {
									panic(err)
								}
								return nil
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								err := d.Set("min_special", 2)
								if err != nil {
									panic(err)
								}
								return nil
							},
							Schema: map[string]*schema.Schema{
								"min_special": {
									Computed: true,
									Type:     schema.TypeInt,
								},

								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config: `resource "random_password" "test" { }`,
				Check:  TestCheckResourceAttr("random_password.test", "min_special", "10"),
			},
			{
				RefreshState: true,
				Check:        TestCheckResourceAttr("random_password.test", "min_special", "2"),
			},
			{
				Config: `resource "random_password" "test" { }`,
				Check:  TestCheckResourceAttr("random_password.test", "min_special", "2"),
			},
		},
	})
}

//nolint:paralleltest // Can't use t.Parallel with t.Setenv
func TestTest_TestStep_ProviderFactories_CopyWorkingDir_EachTestStep(t *testing.T) {
	t.Setenv(plugintest.EnvTfAccPersistWorkingDir, "1")
	workingDir := t.TempDir()

	testSteps := []TestStep{
		{
			Config: `resource "random_password" "test" { }`,
		},
		{
			Config: `resource "random_password" "test" { }`,
		},
	}

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
								d.SetId("id")
								return nil
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		WorkingDir: workingDir,
		Steps:      testSteps,
	})

	workingDirPath := filepath.Dir(workingDir)

	for k := range testSteps {
		dir := workingDirPath + "_" + strconv.Itoa(k+1)

		_, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("cannot read dir: %s", dir)
		}
	}
}

func TestTest_TestStep_ProviderFactories_RefreshWithPlanModifier_Inline(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CustomizeDiff: customdiff.All(
								func(ctx context.Context, d *schema.ResourceDiff, meta interface{}) error {
									special, ok := d.Get("special").(bool)
									if !ok {
										return fmt.Errorf("unexpected type %T for 'special' key", d.Get("special"))
									}

									if special == true {
										err := d.SetNew("special", false)
										if err != nil {
											panic(err)
										}
									}
									return nil
								},
							),
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
								d.SetId("id")
								err := d.Set("special", false)
								if err != nil {
									panic(err)
								}
								return nil
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								t := getTimeForTest()
								if t.After(time.Now().Add(time.Hour * 1)) {
									err := d.Set("special", true)
									if err != nil {
										panic(err)
									}
								}
								return nil
							},
							Schema: map[string]*schema.Schema{
								"special": {
									Computed: true,
									Type:     schema.TypeBool,
									ForceNew: true,
								},

								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config: `resource "random_password" "test" { }`,
				Check:  TestCheckResourceAttr("random_password.test", "special", "false"),
			},
			{
				PreConfig:          setTimeForTest(time.Now().Add(time.Hour * 2)),
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              TestCheckResourceAttr("random_password.test", "special", "true"),
			},
			{
				PreConfig: setTimeForTest(time.Now()),
				Config:    `resource "random_password" "test" { }`,
				Check:     TestCheckResourceAttr("random_password.test", "special", "false"),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_Inline_With_Data_Source(t *testing.T) {
	var id string

	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"http": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					DataSourcesMap: map[string]*schema.Resource{
						"http": {
							ReadContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) (diags diag.Diagnostics) {
								url, ok := d.Get("url").(string)
								if !ok {
									return diag.Errorf("unexpected type %T for 'url' key", d.Get("url"))
								}

								responseHeaders := map[string]string{
									"headerOne":   "one",
									"headerTwo":   "two",
									"headerThree": "three",
									"headerFour":  "four",
								}
								if err := d.Set("response_headers", responseHeaders); err != nil {
									return append(diags, diag.Errorf("Error setting HTTP response headers: %s", err)...)
								}

								d.SetId(url)

								return diags
							},
							Schema: map[string]*schema.Schema{
								"url": {
									Type:     schema.TypeString,
									Required: true,
								},
								"response_headers": {
									Type:     schema.TypeMap,
									Computed: true,
									Elem: &schema.Schema{
										Type: schema.TypeString,
									},
								},
							},
						},
					},
				}, nil
			},
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_string": {
							CreateContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								d.SetId("none")
								err := d.Set("length", 4)
								if err != nil {
									panic(err)
								}
								err = d.Set("result", "none")
								if err != nil {
									panic(err)
								}
								return nil
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"length": {
									Required: true,
									ForceNew: true,
									Type:     schema.TypeInt,
								},
								"result": {
									Type:      schema.TypeString,
									Computed:  true,
									Sensitive: true,
								},

								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
							Importer: &schema.ResourceImporter{
								StateContext: func(ctx context.Context, d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
									val := d.Id()

									d.SetId(val)

									err := d.Set("result", val)
									if err != nil {
										panic(err)
									}

									err = d.Set("length", len(val))
									if err != nil {
										panic(err)
									}

									return []*schema.ResourceData{d}, nil
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config: `data "http" "example" {
							url = "https://checkpoint-api.hashicorp.com/v1/check/terraform"
						}

						resource "random_string" "example" {
							length = length(data.http.example.response_headers)
						}`,
				Check: extractResourceAttr("random_string.example", "id", &id),
			},
			{
				Config: `data "http" "example" {
							url = "https://checkpoint-api.hashicorp.com/v1/check/terraform"
						}

						resource "random_string" "example" {
							length = length(data.http.example.response_headers)
						}`,
				ResourceName: "random_string.example",
				ImportState:  true,
				ImportStateCheck: composeImportStateCheck(
					testCheckResourceAttrInstanceState(&id, "length", "4"),
				),
				ImportStateVerify: true,
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_Import_External_With_Data_Source(t *testing.T) {
	var id string

	t.Parallel()

	Test(t, TestCase{
		ExternalProviders: map[string]ExternalProvider{
			"http": {
				Source: "registry.terraform.io/hashicorp/http",
			},
			"random": {
				Source: "registry.terraform.io/hashicorp/random",
			},
		},
		Steps: []TestStep{
			{
				Config: `data "http" "example" {
							url = "https://checkpoint-api.hashicorp.com/v1/check/terraform"
						}

						resource "random_string" "example" {
							length = length(data.http.example.response_headers)
						}`,
				Check: extractResourceAttr("random_string.example", "id", &id),
			},
			{
				Config: `data "http" "example" {
							url = "https://checkpoint-api.hashicorp.com/v1/check/terraform"
						}

						resource "random_string" "example" {
							length = length(data.http.example.response_headers)
						}`,
				ResourceName: "random_string.example",
				ImportState:  true,
				ImportStateCheck: composeImportStateCheck(
					testCheckResourceAttrInstanceState(&id, "length", "12"),
				),
				ImportStateVerify: true,
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectErrorSummary(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) (diags diag.Diagnostics) {
								d.SetId("id")
								return append(diags, diag.Diagnostic{
									Severity: diag.Error,
									Summary:  "error diagnostic - summary",
								})
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config:      `resource "random_password" "test" { }`,
				ExpectError: regexp.MustCompile(`.*error diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectErrorDetail(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) (diags diag.Diagnostics) {
								d.SetId("id")
								return append(diags, diag.Diagnostic{
									Severity: diag.Error,
									Detail:   "error diagnostic - detail",
								})
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config:      `resource "random_password" "test" { }`,
				ExpectError: regexp.MustCompile(`.*error diagnostic - detail`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectWarningSummary(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) (diags diag.Diagnostics) {
								d.SetId("id")
								return append(diags, diag.Diagnostic{
									Severity: diag.Warning,
									Summary:  "warning diagnostic - summary",
								})
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config:        `resource "random_password" "test" { }`,
				ExpectWarning: regexp.MustCompile(`.*warning diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectWarningDetail(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"random_password": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) (diags diag.Diagnostics) {
								d.SetId("id")
								return append(diags, diag.Diagnostic{
									Severity: diag.Warning,
									Detail:   "warning diagnostic - detail",
								})
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config:        `resource "random_password" "test" { }`,
				ExpectWarning: regexp.MustCompile(`.*warning diagnostic - detail`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectErrorRefresh(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"random_password": {
									CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
										d.SetId("id")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) (diags diag.Diagnostics) {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"id": {
											Computed: true,
											Type:     schema.TypeString,
										},
									},
								},
							},
						}, nil
					},
				},
				Config: `resource "random_password" "test" { }`,
			},
			{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"random_password": {
									CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
										d.SetId("id")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) (diags diag.Diagnostics) {
										return append(diags, diag.Diagnostic{
											Severity: diag.Error,
											Summary:  "error diagnostic - summary",
										})
									},
									Schema: map[string]*schema.Schema{
										"id": {
											Computed: true,
											Type:     schema.TypeString,
										},
									},
								},
							},
						}, nil
					},
				},
				RefreshState: true,
				ExpectError:  regexp.MustCompile(`.*error diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectWarningRefresh(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"random_password": {
									CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
										d.SetId("id")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) (diags diag.Diagnostics) {
										return nil
									},
									Schema: map[string]*schema.Schema{
										"id": {
											Computed: true,
											Type:     schema.TypeString,
										},
									},
								},
							},
						}, nil
					},
				},
				Config: `resource "random_password" "test" { }`,
			},
			{
				ProviderFactories: map[string]func() (*schema.Provider, error){
					"random": func() (*schema.Provider, error) { //nolint:unparam // required signature
						return &schema.Provider{
							ResourcesMap: map[string]*schema.Resource{
								"random_password": {
									CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
										d.SetId("id")
										return nil
									},
									DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
										return nil
									},
									ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) (diags diag.Diagnostics) {
										return append(diags, diag.Diagnostic{
											Severity: diag.Warning,
											Summary:  "warning diagnostic - summary",
										})
									},
									Schema: map[string]*schema.Schema{
										"id": {
											Computed: true,
											Type:     schema.TypeString,
										},
									},
								},
							},
						}, nil
					},
				},
				RefreshState: true,
				// ExpectNonEmptyPlan is set to true otherwise following error is generated:
				// # random_password.test will be destroyed
				// # (because random_password.test is not in configuration)
				ExpectNonEmptyPlan: true,
				ExpectWarning:      regexp.MustCompile(`.*warning diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectErrorPlan(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.Resource{
										MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
											resp.TypeName = req.ProviderTypeName + "_password"
										},
										SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
											resp.Schema = fwresourceschema.Schema{
												Attributes: map[string]fwresourceschema.Attribute{
													"id": fwresourceschema.StringAttribute{
														Computed: true,
													},
												},
											}
										},
										CreateMethod: func(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
											var data struct {
												Id types.String `tfsdk:"id"`
											}

											data.Id = types.StringValue("id")

											diags := resp.State.Set(ctx, &data)
											resp.Diagnostics.Append(diags...)
										},
									}
								},
							}
						},
					}),
				},
				Config: `resource "random_password" "test" { }`,
			},
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.ResourceWithModifyPlan{
										Resource: &testprovider.Resource{
											MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
												resp.TypeName = req.ProviderTypeName + "_password"
											},
											SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
												resp.Schema = fwresourceschema.Schema{
													Attributes: map[string]fwresourceschema.Attribute{
														"id": fwresourceschema.StringAttribute{
															Computed: true,
														},
													},
												}
											},
										},
										ModifyPlanMethod: func(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
											if !req.Plan.Raw.IsNull() {
												resp.Diagnostics.Append(fwdiag.NewErrorDiagnostic("error diagnostic - summary", ""))
											}
										},
									}
								},
							}
						},
					}),
				},
				Config:      `resource "random_password" "test" { }`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`.*error diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectWarningPlan(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.Resource{
										MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
											resp.TypeName = req.ProviderTypeName + "_password"
										},
										SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
											resp.Schema = fwresourceschema.Schema{
												Attributes: map[string]fwresourceschema.Attribute{
													"id": fwresourceschema.StringAttribute{
														Computed: true,
													},
												},
											}
										},
										CreateMethod: func(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
											var data struct {
												Id types.String `tfsdk:"id"`
											}

											data.Id = types.StringValue("example-id")

											diags := resp.State.Set(ctx, &data)
											resp.Diagnostics.Append(diags...)
										},
									}
								},
							}
						},
					}),
				},
				Config: `resource "random_password" "test" { }`,
			},
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.ResourceWithModifyPlan{
										Resource: &testprovider.Resource{
											MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
												resp.TypeName = req.ProviderTypeName + "_password"
											},
											SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
												resp.Schema = fwresourceschema.Schema{
													Attributes: map[string]fwresourceschema.Attribute{
														"id": fwresourceschema.StringAttribute{
															Computed: true,
														},
													},
												}
											},
										},
										ModifyPlanMethod: func(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
											resp.Diagnostics.Append(fwdiag.NewWarningDiagnostic("warning diagnostic - summary", ""))
										},
									}
								},
							}
						},
					}),
				},
				Config:        `resource "random_password" "test" { }`,
				PlanOnly:      true,
				ExpectWarning: regexp.MustCompile(`.*warning diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectErrorDestroy(t *testing.T) {
	t.Parallel()

	deleteCount := 0

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.Resource{
										MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
											resp.TypeName = req.ProviderTypeName + "_password"
										},
										SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
											resp.Schema = fwresourceschema.Schema{
												Attributes: map[string]fwresourceschema.Attribute{
													"id": fwresourceschema.StringAttribute{
														Computed: true,
													},
												},
											}
										},
										CreateMethod: func(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
											var data struct {
												Id types.String `tfsdk:"id"`
											}

											data.Id = types.StringValue("example-id")

											diags := resp.State.Set(ctx, &data)
											resp.Diagnostics.Append(diags...)
										},
									}
								},
							}
						},
					}),
				},
				Config: `resource "random_password" "test" { }`,
			},
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.Resource{
										MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
											resp.TypeName = req.ProviderTypeName + "_password"
										},
										SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
											resp.Schema = fwresourceschema.Schema{
												Attributes: map[string]fwresourceschema.Attribute{
													"id": fwresourceschema.StringAttribute{
														Computed: true,
													},
												},
											}
										},
										DeleteMethod: func(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
											// TODO: deleteCount is used so that when Delete is called during apply, diagnostic is added but
											// when Delete is called during runPostTestDestroy it does not add diagnostic.
											if deleteCount < 1 {
												resp.Diagnostics.Append(fwdiag.NewErrorDiagnostic("error diagnostic - summary", ""))
											}

											deleteCount++
										},
									}
								},
							}
						},
					}),
				},
				Config:      `resource "random_password" "test" { }`,
				Destroy:     true,
				ExpectError: regexp.MustCompile(`.*error diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ExpectWarningDestroy(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		Steps: []TestStep{
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.Resource{
										MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
											resp.TypeName = req.ProviderTypeName + "_password"
										},
										SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
											resp.Schema = fwresourceschema.Schema{
												Attributes: map[string]fwresourceschema.Attribute{
													"id": fwresourceschema.StringAttribute{
														Computed: true,
													},
												},
											}
										},
										CreateMethod: func(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
											var data struct {
												Id types.String `tfsdk:"id"`
											}

											data.Id = types.StringValue("example-id")

											diags := resp.State.Set(ctx, &data)
											resp.Diagnostics.Append(diags...)
										},
									}
								},
							}
						},
					}),
				},
				Config: `resource "random_password" "test" { }`,
			},
			{
				ProtoV5ProviderFactories: map[string]func() (tfprotov5.ProviderServer, error){
					"random": providerserver.NewProtocol5WithError(&testprovider.Provider{
						MetadataMethod: func(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
							resp.TypeName = "random"
						},
						ResourcesMethod: func(ctx context.Context) []func() resource.Resource {
							return []func() resource.Resource{
								func() resource.Resource {
									return &testprovider.Resource{
										MetadataMethod: func(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
											resp.TypeName = req.ProviderTypeName + "_password"
										},
										SchemaMethod: func(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
											resp.Schema = fwresourceschema.Schema{
												Attributes: map[string]fwresourceschema.Attribute{
													"id": fwresourceschema.StringAttribute{
														Computed: true,
													},
												},
											}
										},
										DeleteMethod: func(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
											resp.Diagnostics.Append(fwdiag.NewWarningDiagnostic("warning diagnostic - summary", ""))
										},
									}
								},
							}
						},
					}),
				},
				Config:        `resource "random_password" "test" { }`,
				Destroy:       true,
				ExpectWarning: regexp.MustCompile(`.*warning diagnostic - summary`),
			},
		},
	})
}

func TestTest_TestStep_ProviderFactories_ErrorCheck(t *testing.T) {
	t.Parallel()

	Test(t, TestCase{
		ErrorCheck: func(err error) error {
			r := regexp.MustCompile("error summary")

			if r.MatchString(err.Error()) {
				return nil
			}

			return err
		},
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"example": func() (*schema.Provider, error) { //nolint:unparam // required signature
				return &schema.Provider{
					ResourcesMap: map[string]*schema.Resource{
						"example_resource": {
							CreateContext: func(ctx context.Context, d *schema.ResourceData, i interface{}) diag.Diagnostics {
								d.SetId("id")

								return diag.Diagnostics{
									diag.Diagnostic{
										Severity: 0,
										Summary:  "error summary",
										Detail:   "error detail",
									},
								}
							},
							DeleteContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							ReadContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
								return nil
							},
							Schema: map[string]*schema.Schema{
								"id": {
									Computed: true,
									Type:     schema.TypeString,
								},
							},
						},
					},
				}, nil
			},
		},
		Steps: []TestStep{
			{
				Config: `resource "example_resource" "test" { }`,
			},
		},
	})
}

func setTimeForTest(t time.Time) func() {
	return func() {
		getTimeForTest = func() time.Time {
			return t
		}
	}
}

var getTimeForTest = func() time.Time {
	return time.Now()
}

func composeImportStateCheck(fs ...ImportStateCheckFunc) ImportStateCheckFunc {
	return func(s []*terraform.InstanceState) error {
		for i, f := range fs {
			if err := f(s); err != nil {
				return fmt.Errorf("check %d/%d error: %s", i+1, len(fs), err)
			}
		}

		return nil
	}
}

//nolint:unparam // Generic test function
func testExtractResourceAttrInstanceState(id, attributeName string, attributeValue *string) ImportStateCheckFunc {
	return func(is []*terraform.InstanceState) error {
		for _, v := range is {
			if v.ID != id {
				continue
			}

			if attrVal, ok := v.Attributes[attributeName]; ok {
				*attributeValue = attrVal

				return nil
			}
		}

		return fmt.Errorf("attribute %s not found in instance state", attributeName)
	}
}

func testCheckResourceAttrInstanceState(id *string, attributeName, attributeValue string) ImportStateCheckFunc {
	return func(is []*terraform.InstanceState) error {
		for _, v := range is {
			if v.ID != *id {
				continue
			}

			if attrVal, ok := v.Attributes[attributeName]; ok {
				if attrVal != attributeValue {
					return fmt.Errorf("expected: %s got: %s", attributeValue, attrVal)
				}

				return nil
			}
		}

		return fmt.Errorf("attribute %s not found in instance state", attributeName)
	}
}

//nolint:unparam // Generic test function
func testExtractResourceAttr(resourceName string, attributeName string, attributeValue *string) TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]

		if !ok {
			return fmt.Errorf("resource name %s not found in state", resourceName)
		}

		attrValue, ok := rs.Primary.Attributes[attributeName]

		if !ok {
			return fmt.Errorf("attribute %s not found in resource %s state", attributeName, resourceName)
		}

		*attributeValue = attrValue

		return nil
	}
}

func testCheckAttributeValuesEqual(i *string, j *string) TestCheckFunc {
	return func(s *terraform.State) error {
		if testStringValue(i) != testStringValue(j) {
			return fmt.Errorf("attribute values are different, got %s and %s", testStringValue(i), testStringValue(j))
		}

		return nil
	}
}

func testCheckAttributeValuesDiffer(i *string, j *string) TestCheckFunc {
	return func(s *terraform.State) error {
		if testStringValue(i) == testStringValue(j) {
			return fmt.Errorf("attribute values are the same")
		}

		return nil
	}
}

func testStringValue(sPtr *string) string {
	if sPtr == nil {
		return ""
	}

	return *sPtr
}
