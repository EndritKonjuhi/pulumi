// Copyright 2025, Pulumi Corporation.
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

package engine

import (
	"reflect"
	"slices"
	"testing"

	pkgresource "github.com/pulumi/pulumi/pkg/v3/resource"

	"github.com/pulumi/pulumi/pkg/v3/resource/deploy"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/require"
)

func TestSameStepMustWrite(t *testing.T) {
	t.Parallel()

	defaultURN := resource.URN("urn:pulumi:test::stack::pulumi:providers:aws::default_4_42_0")

	cases := []struct {
		name      string
		old       *pkgresource.State
		new       *pkgresource.State
		mustWrite bool
	}{
		{
			name:      "changed URN",
			old:       &pkgresource.State{URN: defaultURN},
			new:       &pkgresource.State{URN: "urn:pulumi:test::stack::pulumi:providers:aws::default_4_42_1"},
			mustWrite: true,
		},
		{
			name:      "type changed",
			old:       &pkgresource.State{URN: defaultURN, Type: "pulumi:providers:aws"},
			new:       &pkgresource.State{URN: defaultURN, Type: "pulumi:providers:awsx"},
			mustWrite: true,
		},
		{
			name:      "custom changed",
			old:       &pkgresource.State{URN: defaultURN, Type: "pulumi:providers:aws", ID: "a", Custom: true},
			new:       &pkgresource.State{URN: defaultURN, Type: "pulumi:providers:aws", ID: "", Custom: false},
			mustWrite: true,
		},
		{
			name:      "custom timeout changed",
			old:       &pkgresource.State{URN: defaultURN, CustomTimeouts: resource.CustomTimeouts{Create: 600}},
			new:       &pkgresource.State{URN: defaultURN, CustomTimeouts: resource.CustomTimeouts{Create: 900}},
			mustWrite: true,
		},
		{
			name:      "RetainOnDelete changed",
			old:       &pkgresource.State{URN: defaultURN, RetainOnDelete: false},
			new:       &pkgresource.State{URN: defaultURN, RetainOnDelete: true},
			mustWrite: true,
		},
		{
			name: "Provider changed",
			old: &pkgresource.State{
				URN:      defaultURN,
				Provider: "urn:pulumi:test::stack::pulumi:providers:aws::default_4_42_0::provider-id-1",
			},
			new: &pkgresource.State{
				URN:      defaultURN,
				Provider: "urn:pulumi:test::stack::pulumi:providers:aws::default_4_42_0::provider-id-2",
			},
			mustWrite: true,
		},
		{
			name:      "Parent changed",
			old:       &pkgresource.State{URN: defaultURN, Parent: "urn:pulumi:test::stack::pulumi:pulumi:Stack::parent1"},
			new:       &pkgresource.State{URN: defaultURN, Parent: "urn:pulumi:test::stack::pulumi:pulumi:Stack::parent2"},
			mustWrite: true,
		},
		{
			name: "DeletedWith changed",
			old: &pkgresource.State{
				URN:         defaultURN,
				DeletedWith: "urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
			},
			new: &pkgresource.State{
				URN:         defaultURN,
				DeletedWith: "urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2",
			},
			mustWrite: true,
		},
		{
			name: "ReplaceWith changed - different length",
			old: &pkgresource.State{
				URN:         defaultURN,
				ReplaceWith: []resource.URN{"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
			},
			new:       &pkgresource.State{URN: defaultURN, ReplaceWith: []resource.URN{}},
			mustWrite: true,
		},
		{
			name: "ReplaceWith changed - different content",
			old: &pkgresource.State{URN: defaultURN, ReplaceWith: []resource.URN{
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
			}},
			new: &pkgresource.State{URN: defaultURN, ReplaceWith: []resource.URN{
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2",
			}},
			mustWrite: true,
		},
		{
			name:      "Protect changed",
			old:       &pkgresource.State{URN: defaultURN, Protect: false},
			new:       &pkgresource.State{URN: defaultURN, Protect: true},
			mustWrite: true,
		},
		{
			name: "Inputs changed",
			old: &pkgresource.State{
				URN:    defaultURN,
				Inputs: resource.PropertyMap{"key": resource.NewProperty("value1")},
			},
			new: &pkgresource.State{
				URN:    defaultURN,
				Inputs: resource.PropertyMap{"key": resource.NewProperty("value2")},
			},
			mustWrite: true,
		},
		{
			name: "Outputs changed",
			old: &pkgresource.State{
				URN:     defaultURN,
				Outputs: resource.PropertyMap{"key": resource.NewProperty("value1")},
			},
			new: &pkgresource.State{
				URN:     defaultURN,
				Outputs: resource.PropertyMap{"key": resource.NewProperty("value2")},
			},
			mustWrite: true,
		},
		{
			name: "Dependencies changed - added dependency",
			old:  &pkgresource.State{URN: defaultURN, Dependencies: []resource.URN{}},
			new: &pkgresource.State{URN: defaultURN, Dependencies: []resource.URN{
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
			}},
			mustWrite: true,
		},
		{
			name: "Dependencies changed - removed dependency",
			old: &pkgresource.State{URN: defaultURN, Dependencies: []resource.URN{
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
			}},
			new:       &pkgresource.State{URN: defaultURN, Dependencies: []resource.URN{}},
			mustWrite: true,
		},
		{
			name: "Dependencies changed - different order (should not trigger write)",
			old: &pkgresource.State{URN: defaultURN, Dependencies: []resource.URN{
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2",
			}},
			new: &pkgresource.State{URN: defaultURN, Dependencies: []resource.URN{
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2",
				"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
			}},
			mustWrite: false,
		},
		{
			name: "PropertyDependencies changed - added property",
			old: &pkgresource.State{
				URN:                  defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{},
			},
			new: &pkgresource.State{
				URN: defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
			},
			mustWrite: true,
		},
		{
			name: "PropertyDependencies changed - removed property",
			old: &pkgresource.State{
				URN: defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
			},
			new: &pkgresource.State{
				URN:                  defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{},
			},
			mustWrite: true,
		},
		{
			name: "PropertyDependencies changed - missing property",
			old: &pkgresource.State{
				URN: defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
			},
			new: &pkgresource.State{
				URN: defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop2": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
			},
			mustWrite: true,
		},
		{
			name: "PropertyDependencies key with empty list added - should not write",
			old: &pkgresource.State{
				URN:                  defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{},
			},
			new: &pkgresource.State{
				URN:                  defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{"prop1": {}},
			},
			mustWrite: false,
		},
		{
			name: "PropertyDependencies key with empty list removed - should not write",
			old: &pkgresource.State{
				URN:                  defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{"prop1": {}},
			},
			new: &pkgresource.State{
				URN:                  defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{},
			},
			mustWrite: false,
		},
		{
			name: "PropertyDependencies changed - different dependencies for same property",
			old: &pkgresource.State{
				URN: defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
					"prop2": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
			},
			new: &pkgresource.State{
				URN: defaultURN,
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2"},
					"prop2": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
			},
			mustWrite: true,
		},
		{
			name: "ResourceHooks changed",
			old: &pkgresource.State{URN: defaultURN, ResourceHooks: map[resource.HookType][]string{
				resource.BeforeCreate: {"hook1"},
			}},
			new: &pkgresource.State{URN: defaultURN, ResourceHooks: map[resource.HookType][]string{
				resource.BeforeCreate: {"hook2"},
			}},
			mustWrite: true,
		},
		{
			name:      "ReplacementTrigger changed",
			old:       &pkgresource.State{URN: defaultURN, ReplacementTrigger: property.New("trigger1")},
			new:       &pkgresource.State{URN: defaultURN, ReplacementTrigger: property.New("trigger2")},
			mustWrite: true,
		},
		{
			name: "PropertyDependencies changed - different order (should not trigger write)",
			old: &pkgresource.State{URN: defaultURN, PropertyDependencies: map[resource.PropertyKey][]resource.URN{
				"prop1": {
					"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
					"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2",
				},
			}},
			new: &pkgresource.State{URN: defaultURN, PropertyDependencies: map[resource.PropertyKey][]resource.URN{
				"prop1": {
					"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource2",
					"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1",
				},
			}},
			mustWrite: false,
		},
		{
			name: "ReplacementTrigger dependencies changed - should not write",
			old: &pkgresource.State{URN: defaultURN, ReplacementTrigger: property.New("trigger").WithDependencies(
				[]resource.URN{"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"})},
			new:       &pkgresource.State{URN: defaultURN, ReplacementTrigger: property.New("trigger")},
			mustWrite: false,
		},
		{
			name:      "ReplacementTrigger secretness changed",
			old:       &pkgresource.State{URN: defaultURN, ReplacementTrigger: property.New("trigger")},
			new:       &pkgresource.State{URN: defaultURN, ReplacementTrigger: property.New("trigger").WithSecret(true)},
			mustWrite: true,
		},
		{
			name:      "Taint changed",
			old:       &pkgresource.State{URN: defaultURN, Taint: true},
			new:       &pkgresource.State{URN: defaultURN},
			mustWrite: true,
		},
		{
			name:      "PendingReplacement changed",
			old:       &pkgresource.State{URN: defaultURN, PendingReplacement: true},
			new:       &pkgresource.State{URN: defaultURN},
			mustWrite: true,
		},
		{
			name:      "RefreshBeforeUpdate changed",
			old:       &pkgresource.State{URN: defaultURN},
			new:       &pkgresource.State{URN: defaultURN, RefreshBeforeUpdate: true},
			mustWrite: true,
		},
		{
			name:      "ExtensionRef changed",
			old:       &pkgresource.State{URN: defaultURN, ExtensionRef: "ref1"},
			new:       &pkgresource.State{URN: defaultURN, ExtensionRef: "ref2"},
			mustWrite: true,
		},
		{
			name:      "ImportID changed",
			old:       &pkgresource.State{URN: defaultURN, ImportID: "id1"},
			new:       &pkgresource.State{URN: defaultURN},
			mustWrite: true,
		},
		{
			name:      "ViewOf changed",
			old:       &pkgresource.State{URN: defaultURN},
			new:       &pkgresource.State{URN: defaultURN, ViewOf: "urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
			mustWrite: true,
		},
		{
			name:      "SnippetID changed",
			old:       &pkgresource.State{URN: defaultURN, SnippetID: "snippet1"},
			new:       &pkgresource.State{URN: defaultURN, SnippetID: "snippet2"},
			mustWrite: true,
		},
		{
			name: "Aliases changed",
			old:  &pkgresource.State{URN: defaultURN},
			new: &pkgresource.State{
				URN:     defaultURN,
				Aliases: []resource.URN{"urn:pulumi:test::stack::pulumi:pulumi:Stack::old"},
			},
			mustWrite: true,
		},
		{
			name:      "AdditionalSecretOutputs changed",
			old:       &pkgresource.State{URN: defaultURN},
			new:       &pkgresource.State{URN: defaultURN, AdditionalSecretOutputs: []resource.PropertyKey{"prop1"}},
			mustWrite: true,
		},
		{
			name:      "IgnoreChanges changed",
			old:       &pkgresource.State{URN: defaultURN},
			new:       &pkgresource.State{URN: defaultURN, IgnoreChanges: []string{"prop1"}},
			mustWrite: true,
		},
		{
			name:      "IgnoreChanges changed - different order (should not trigger write)",
			old:       &pkgresource.State{URN: defaultURN, IgnoreChanges: []string{"prop1", "prop2"}},
			new:       &pkgresource.State{URN: defaultURN, IgnoreChanges: []string{"prop2", "prop1"}},
			mustWrite: false,
		},
		{
			name:      "ReplaceOnChanges changed",
			old:       &pkgresource.State{URN: defaultURN, ReplaceOnChanges: []string{"prop1"}},
			new:       &pkgresource.State{URN: defaultURN},
			mustWrite: true,
		},
		{
			name:      "HideDiff changed",
			old:       &pkgresource.State{URN: defaultURN},
			new:       &pkgresource.State{URN: defaultURN, HideDiff: []resource.PropertyPath{{"prop1", 0}}},
			mustWrite: true,
		},
		{
			name:      "SourcePosition changed - should not write",
			old:       &pkgresource.State{URN: defaultURN, SourcePosition: "pos1"},
			new:       &pkgresource.State{URN: defaultURN, SourcePosition: "pos2"},
			mustWrite: false,
		},
		{
			name: "nothing changed",
			old: &pkgresource.State{
				URN:          defaultURN,
				Type:         "pulumi:providers:aws",
				Inputs:       resource.PropertyMap{"key": resource.NewProperty("value")},
				Outputs:      resource.PropertyMap{"key": resource.NewProperty("value")},
				Dependencies: []resource.URN{"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
				ResourceHooks:  map[resource.HookType][]string{resource.BeforeCreate: {"hook1"}},
				SourcePosition: "pos1",
			},
			new: &pkgresource.State{
				URN:          defaultURN,
				Type:         "pulumi:providers:aws",
				Inputs:       resource.PropertyMap{"key": resource.NewProperty("value")},
				Outputs:      resource.PropertyMap{"key": resource.NewProperty("value")},
				Dependencies: []resource.URN{"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				PropertyDependencies: map[resource.PropertyKey][]resource.URN{
					"prop1": {"urn:pulumi:test::stack::pulumi:pulumi:Stack::resource1"},
				},
				ResourceHooks:  map[resource.HookType][]string{resource.BeforeCreate: {"hook1"}},
				SourcePosition: "pos1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			step := deploy.NewSameStep(nil, nil, tc.old, tc.new)
			result := SameStepMustWrite(step)
			require.Equal(t, tc.mustWrite, result)
		})
	}
}

// TestSameStepMustWriteCoversEveryField fails when resource.State gets a field that SameStepMustWrite neither compares
// nor deliberately ignores.
func TestSameStepMustWriteCoversEveryField(t *testing.T) {
	t.Parallel()

	compared := []string{
		"Type", "URN", "Custom", "Inputs", "Outputs", "Parent", "Protect", "Taint", "Dependencies", "Provider",
		"ExtensionRef", "PropertyDependencies", "PendingReplacement", "AdditionalSecretOutputs", "Aliases",
		"CustomTimeouts", "ImportID", "RetainOnDelete", "DeletedWith", "ReplaceWith", "IgnoreChanges",
		"ReplaceOnChanges", "ReplacementTrigger", "HideDiff", "RefreshBeforeUpdate", "ViewOf", "ResourceHooks",
		"SnippetID",
		// SameStepMustWrite asserts that these are equal.
		"Delete", "External", "ID",
	}
	ignored := []string{"Lock", "InitErrors", "Created", "Modified", "SourcePosition", "StackTrace"}

	stateType := reflect.TypeFor[pkgresource.State]()
	fields := make([]string, 0, stateType.NumField())
	for field := range stateType.Fields() {
		fields = append(fields, field.Name)
	}
	require.ElementsMatch(t, slices.Concat(compared, ignored), fields)
}
