// Copyright 2026, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !all

package main

import (
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type component struct {
	pulumi.ResourceState
}

// The environment variables select what changes: EXTRA_RESOURCE adds a resource, STACK_OUTPUT sets a stack output,
// and COMPONENT_OUTPUT sets an output of a component, which changes only the state.
func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		comp := &component{}
		if err := ctx.RegisterComponentResource("test:index:Component", "comp", comp); err != nil {
			return err
		}
		err := ctx.RegisterResourceOutputs(comp, pulumi.Map{"out": pulumi.String(os.Getenv("COMPONENT_OUTPUT"))})
		if err != nil {
			return err
		}

		if os.Getenv("EXTRA_RESOURCE") != "" {
			if err := ctx.RegisterComponentResource("test:index:Component", "extra", &component{}); err != nil {
				return err
			}
		}

		ctx.Export("out", pulumi.String(os.Getenv("STACK_OUTPUT")))
		return nil
	})
}
