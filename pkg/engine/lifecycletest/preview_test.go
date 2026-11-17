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

package lifecycletest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/blang/semver"
	"github.com/gofrs/uuid"
	"github.com/pulumi/pulumi/pkg/v3/backend"
	"github.com/pulumi/pulumi/pkg/v3/display"
	"github.com/pulumi/pulumi/pkg/v3/engine"
	. "github.com/pulumi/pulumi/pkg/v3/engine"
	lt "github.com/pulumi/pulumi/pkg/v3/engine/lifecycletest/framework"
	pkgresource "github.com/pulumi/pulumi/pkg/v3/resource"
	"github.com/pulumi/pulumi/pkg/v3/resource/deploy"
	"github.com/pulumi/pulumi/pkg/v3/resource/deploy/deploytest"
	"github.com/pulumi/pulumi/pkg/v3/resource/plugin"
	"github.com/pulumi/pulumi/pkg/v3/resource/stack"
	"github.com/pulumi/pulumi/pkg/v3/secrets/b64"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test that we can run a preview --refresh --run-program by executing the program for it the refresh stage.
func TestPreviewRefreshWithProgram(t *testing.T) {
	t.Parallel()

	programInputs := property.NewMap(map[string]property.Value{"foo": property.New("bar")})
	createOutputs := property.NewMap(map[string]property.Value{"foo": property.New("bar")})
	updateOutputs := property.NewMap(map[string]property.Value{"foo": property.New("qux")})
	readOutputs := property.NewMap(map[string]property.Value{"foo": property.New("baz")})

	loaders := []*deploytest.ProviderLoader{
		deploytest.NewProviderLoader("pkgA", semver.MustParse("1.0.0"), func() (plugin.Provider, error) {
			return &deploytest.Provider{
				ReadF: func(_ context.Context, req plugin.ReadRequest) (plugin.ReadResponse, error) {
					if req.Name == "resA" {
						assert.Equal(t, createOutputs, req.Inputs)
						assert.Equal(t, createOutputs, req.State)

						return plugin.ReadResponse{
							ReadResult: plugin.ReadResult{
								ID:      req.ID,
								Inputs:  &req.Inputs,
								Outputs: &readOutputs,
							},
							Status: resource.StatusOK,
						}, nil
					}

					return plugin.ReadResponse{
						ReadResult: plugin.ReadResult{
							ID:      req.ID,
							Inputs:  new(property.Map{}),
							Outputs: new(property.Map{}),
						},
						Status: resource.StatusOK,
					}, nil
				},
				CreateF: func(_ context.Context, req plugin.CreateRequest) (plugin.CreateResponse, error) {
					uuid, err := uuid.NewV4()
					if err != nil {
						return plugin.CreateResponse{}, err
					}

					if req.Name == "resA" {
						assert.Equal(t, programInputs, req.Properties)

						return plugin.CreateResponse{
							ID:         resource.ID(uuid.String()),
							Properties: createOutputs,
							Status:     resource.StatusOK,
						}, nil
					}

					return plugin.CreateResponse{
						ID:         resource.ID(uuid.String()),
						Properties: property.Map{},
						Status:     resource.StatusOK,
					}, nil
				},
				UpdateF: func(_ context.Context, req plugin.UpdateRequest) (plugin.UpdateResponse, error) {
					assert.True(t, req.Preview)

					if req.Name == "resA" {
						// This should get called as part of the preview _after_ refresh so we should see the
						// program inputs and the read outputs.
						assert.Equal(t, programInputs, req.NewInputs)
						assert.Equal(t, readOutputs, req.OldOutputs)

						return plugin.UpdateResponse{
							Properties: updateOutputs,
							Status:     resource.StatusOK,
						}, nil
					}

					return plugin.UpdateResponse{
						Properties: property.Map{},
						Status:     resource.StatusOK,
					}, nil
				},
			}, nil
		}),
	}

	programExecutions := 0
	programF := deploytest.NewLanguageRuntimeF(func(_ plugin.RunInfo, monitor *deploytest.ResourceMonitor) error {
		programExecutions++

		resp, err := monitor.RegisterResource("pkgA:m:typA", "resA", true, deploytest.ResourceOptions{
			Inputs: programInputs,
		})
		require.NoError(t, err)

		// First time we should see the create outputs, second time the update outputs
		if programExecutions == 1 {
			assert.Equal(t, createOutputs, resp.Outputs)
		} else {
			assert.Equal(t, updateOutputs, resp.Outputs)
		}

		return nil
	})
	hostF := deploytest.NewPluginHostF(nil, nil, programF, nil, nil, loaders...)

	p := &lt.TestPlan{
		Options: lt.TestUpdateOptions{
			T:     t,
			HostF: hostF,
		},
	}

	// Run an update to create the initial state.
	snap, err := lt.TestOp(Update).
		RunStep(p.GetProject(), p.GetTarget(t, nil), p.Options, false, p.BackendClient, nil, "0")
	require.NoError(t, err)
	assert.Equal(t, 1, programExecutions)
	assert.Equal(t, createOutputs, resource.FromResourcePropertyMap(snap.Resources[1].Inputs))
	assert.Equal(t, createOutputs, resource.FromResourcePropertyMap(snap.Resources[1].Outputs))

	// Change the program inputs to check we don't send changed inputs to the provider for refresh
	programInputs = programInputs.Set("foo", property.New("qux"))
	// Run a preview with refresh
	p.Options.Refresh = true
	p.Options.RefreshProgram = true
	_, err = lt.TestOp(Update).
		RunStep(p.GetProject(), p.GetTarget(t, snap), p.Options, true, p.BackendClient,
			func(project workspace.Project, target deploy.Target, entries engine.JournalEntries,
				events []engine.Event, err error,
			) error {
				return err
			}, "1")
	require.NoError(t, err)
	// Should have run the program again
	assert.Equal(t, 2, programExecutions)
}

// TestPreviewStackOutputs tests that stack outputs are correctly shown during a preview operation.
func TestPreviewStackOutputs(t *testing.T) {
	t.Parallel()

	var step int

	loaders := []*deploytest.ProviderLoader{
		deploytest.NewProviderLoader("pkgA", semver.MustParse("1.0.0"), func() (plugin.Provider, error) {
			return &deploytest.Provider{}, nil
		}),
	}

	programF := deploytest.NewLanguageRuntimeF(func(_ plugin.RunInfo, monitor *deploytest.ResourceMonitor) error {
		resp, err := monitor.RegisterResource(resource.RootStackType, "test", false)
		require.NoError(t, err)

		var outputs property.Map

		switch step {
		case 0:
			outputs = property.NewMap(map[string]property.Value{
				"first":  property.New("abc"),
				"second": property.New("def"),
			})
		case 1:
			// Changes to "first" should be shown in the preview.
			// Changes to "second" should not be shown in the preview, as it is not changed.
			outputs = property.NewMap(map[string]property.Value{
				"first":  property.New("step 1"),
				"second": property.New("def"),
			})
		case 2:
			// We're changing "first" to an unknown, so it should not be shown in the preview.
			outputs = property.NewMap(map[string]property.Value{
				"first":  property.New(property.Computed),
				"second": property.New("def"),
			})
		}

		err = monitor.RegisterResourceOutputs(resp.URN, outputs)
		require.NoError(t, err)

		return nil
	})

	hostF := deploytest.NewPluginHostF(nil, nil, programF, nil, nil, loaders...)
	p := &lt.TestPlan{
		Options: lt.TestUpdateOptions{T: t, HostF: hostF},
	}

	// Run the initial update which sets some stack outputs.
	snap, err := lt.TestOp(Update).
		RunStep(p.GetProject(), p.GetTarget(t, nil), p.Options, false, p.BackendClient, nil, "0")
	require.NoError(t, err)

	// Run a preview that ensures the stack outputs are shown correctly.
	step = 1
	snap, err = lt.TestOp(Update).
		RunStep(p.GetProject(), p.GetTarget(t, snap), p.Options, true, p.BackendClient, nil, "1")
	require.NoError(t, err)

	// Run another preview, this time with an unknown output, that should not be shown.
	step = 2
	_, err = lt.TestOp(Update).
		RunStep(p.GetProject(), p.GetTarget(t, snap), p.Options, true, p.BackendClient, nil, "2")
	require.NoError(t, err)
}

// TestPreviewChanges tests that a preview reports the stack output and state changes that an update would make
// without any resource change.
func TestPreviewChanges(t *testing.T) {
	t.Parallel()

	type knobs struct {
		stackOutput     string
		componentOutput string
		retainOnDelete  bool
		drift           bool
		newResource     bool
		readProviderB   bool
	}

	cases := []struct {
		name       string
		change     func(k *knobs, snap *deploy.Snapshot)
		refresh    bool
		targetResA bool
		want       PreviewChanges
	}{
		{
			name:   "nothing",
			change: func(*knobs, *deploy.Snapshot) {},
		},
		{
			name:    "refresh without drift",
			change:  func(*knobs, *deploy.Snapshot) {},
			refresh: true,
		},
		{
			name:   "stack output",
			change: func(k *knobs, _ *deploy.Snapshot) { k.stackOutput = "changed" },
			want:   PreviewChanges{StackOutputs: true, State: true},
		},
		{
			name:   "component output",
			change: func(k *knobs, _ *deploy.Snapshot) { k.componentOutput = "changed" },
			want:   PreviewChanges{State: true},
		},
		{
			name:   "retain on delete",
			change: func(k *knobs, _ *deploy.Snapshot) { k.retainOnDelete = true },
			want:   PreviewChanges{State: true},
		},
		{
			name:    "refresh with drift",
			change:  func(k *knobs, _ *deploy.Snapshot) { k.drift = true },
			refresh: true,
			want:    PreviewChanges{State: true},
		},
		{
			name:   "read provider",
			change: func(k *knobs, _ *deploy.Snapshot) { k.readProviderB = true },
			want:   PreviewChanges{State: true},
		},
		{
			name: "pending update operation",
			change: func(_ *knobs, snap *deploy.Snapshot) {
				for _, res := range snap.Resources {
					if res.URN.Name() == "resA" {
						snap.PendingOperations = []pkgresource.Operation{
							pkgresource.NewOperation(res, pkgresource.OperationTypeUpdating),
						}
					}
				}
			},
			want: PreviewChanges{State: true},
		},
		{
			name: "pending create operation",
			change: func(_ *knobs, snap *deploy.Snapshot) {
				for _, res := range snap.Resources {
					if res.URN.Name() == "resA" {
						pending := res.Copy()
						pending.URN = res.URN.Rename("resPending")
						snap.PendingOperations = []pkgresource.Operation{
							pkgresource.NewOperation(pending, pkgresource.OperationTypeCreating),
						}
					}
				}
			},
		},
		{
			name: "removed pending replacement",
			change: func(_ *knobs, snap *deploy.Snapshot) {
				for _, res := range snap.Resources {
					if res.URN.Name() == "resA" {
						pending := res.Copy()
						pending.URN = res.URN.Rename("resPendingReplacement")
						pending.PendingReplacement = true
						snap.Resources = append(snap.Resources, pending)
						break
					}
				}
			},
			want: PreviewChanges{State: true},
		},
		{
			name: "migrated provider outputs",
			change: func(_ *knobs, snap *deploy.Snapshot) {
				for _, res := range snap.Resources {
					if res.URN.Name() == "provA" {
						res.Outputs = resource.PropertyMap{}
					}
				}
			},
			want: PreviewChanges{State: true},
		},
		{
			name:       "untargeted new resource",
			change:     func(k *knobs, _ *deploy.Snapshot) { k.newResource = true },
			targetResA: true,
		},
		{
			name: "integrity error metadata",
			change: func(_ *knobs, snap *deploy.Snapshot) {
				snap.Metadata.IntegrityErrorMetadata = &deploy.SnapshotIntegrityErrorMetadata{Error: "broken"}
			},
			want: PreviewChanges{State: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			k := knobs{stackOutput: "original", componentOutput: "original"}
			loaders := []*deploytest.ProviderLoader{
				deploytest.NewProviderLoader("pkgA", semver.MustParse("1.0.0"), func() (plugin.Provider, error) {
					return &deploytest.Provider{
						ParameterizeF: func(
							_ context.Context, req plugin.ParameterizeRequest,
						) (plugin.ParameterizeResponse, error) {
							value := req.Parameters.(*plugin.ParameterizeValue)
							return plugin.ParameterizeResponse{Name: value.Name, Version: value.Version}, nil
						},
						ReadF: func(_ context.Context, req plugin.ReadRequest) (plugin.ReadResponse, error) {
							outputs := req.State
							if k.drift {
								outputs = property.NewMap(map[string]property.Value{"drift": property.New(true)})
							}
							return plugin.ReadResponse{
								ReadResult: plugin.ReadResult{ID: req.ID, Inputs: &req.Inputs, Outputs: &outputs},
								Status:     resource.StatusOK,
							}, nil
						},
					}, nil
				}),
				deploytest.NewProviderLoader("pkgB", semver.MustParse("1.0.0"), func() (plugin.Provider, error) {
					return &deploytest.Provider{
						ReadF: func(_ context.Context, req plugin.ReadRequest) (plugin.ReadResponse, error) {
							return plugin.ReadResponse{
								ReadResult: plugin.ReadResult{ID: req.ID, Inputs: &req.Inputs, Outputs: &req.State},
								Status:     resource.StatusOK,
							}, nil
						},
					}, nil
				}),
			}
			programF := deploytest.NewLanguageRuntimeF(func(_ plugin.RunInfo, monitor *deploytest.ResourceMonitor) error {
				stack, err := monitor.RegisterResource(resource.RootStackType, "test", false)
				require.NoError(t, err)
				_, err = monitor.RegisterResource("pkgA:m:typA", "resA", true, deploytest.ResourceOptions{
					Parent:         stack.URN,
					Inputs:         property.NewMap(map[string]property.Value{"foo": property.New("bar")}),
					RetainOnDelete: &k.retainOnDelete,
				})
				require.NoError(t, err)
				if k.newResource {
					_, err = monitor.RegisterResource("pkgA:m:typA", "resB", true, deploytest.ResourceOptions{
						Parent: stack.URN,
					})
					require.NoError(t, err)
				}
				component, err := monitor.RegisterResource("my:component:Comp", "comp", false, deploytest.ResourceOptions{
					Parent: stack.URN,
				})
				require.NoError(t, err)
				err = monitor.RegisterResourceOutputs(component.URN, property.NewMap(map[string]property.Value{
					"out": property.New(k.componentOutput),
				}))
				require.NoError(t, err)

				ext, err := monitor.RegisterPackage("pkgA", "1.0.0", "", nil, nil, &pulumirpc.Parameterization{
					Name: "ext", Version: "1.0.0", Value: []byte("blob"),
				})
				require.NoError(t, err)
				_, err = monitor.RegisterResource("pkgA:m:typA", "resExt", true, deploytest.ResourceOptions{
					Parent:     stack.URN,
					PackageRef: ext,
				})
				require.NoError(t, err)

				provA, err := monitor.RegisterResource("pulumi:providers:pkgB", "provA", true, deploytest.ResourceOptions{
					Parent: stack.URN,
					Inputs: property.NewMap(map[string]property.Value{"foo": property.New("bar")}),
				})
				require.NoError(t, err)
				provB, err := monitor.RegisterResource("pulumi:providers:pkgB", "provB", true, deploytest.ResourceOptions{
					Parent: stack.URN,
				})
				require.NoError(t, err)
				readProvider := provA
				if k.readProviderB {
					readProvider = provB
				}
				_, _, err = monitor.ReadResource("pkgB:m:typB", "resRead", "read-id", stack.URN, property.Map{},
					string(readProvider.URN)+"::"+string(readProvider.ID), "", "", nil, "", "")
				require.NoError(t, err)

				return monitor.RegisterResourceOutputs(stack.URN, property.NewMap(map[string]property.Value{
					"out": property.New(k.stackOutput),
				}))
			})
			p := &lt.TestPlan{
				Options: lt.TestUpdateOptions{
					T:                t,
					HostF:            deploytest.NewPluginHostF(nil, nil, programF, nil, nil, loaders...),
					SkipDisplayTests: true,
				},
			}

			// The second update deletes a default provider that the first update stores for the extension.
			var snap *deploy.Snapshot
			for _, name := range []string{"0", "1"} {
				var err error
				snap, err = lt.TestOp(Update).
					RunStep(p.GetProject(), p.GetTarget(t, snap), p.Options, false, p.BackendClient, nil, name)
				require.NoError(t, err)
			}

			tc.change(&k, snap)
			base, _, _, err := stack.SerializeDeploymentWithMetadata(t.Context(), snap, false)
			require.NoError(t, err)
			p.Options.Refresh = tc.refresh
			if tc.targetResA {
				for _, res := range snap.Resources {
					if res.URN.Name() == "resA" {
						p.Options.Targets = deploy.NewUrnTargetsFromUrns([]resource.URN{res.URN})
					}
				}
			}
			var previewChanges PreviewChanges
			p.Options.PreviewChanges = &previewChanges
			var resourceChanges display.ResourceChanges
			preview := lt.TestOp(func(
				info UpdateInfo, ctx *Context, opts UpdateOptions, dryRun bool,
			) (*deploy.Plan, display.ResourceChanges, error) {
				plan, changes, err := Update(info, ctx, opts, dryRun)
				resourceChanges = changes
				return plan, changes, err
			})
			_, err = preview.RunStep(p.GetProject(), p.GetTarget(t, snap), p.Options, true, p.BackendClient, nil, "2")
			require.NoError(t, err)

			assert.False(t, HasChanges(resourceChanges))
			assert.Equal(t, tc.want, previewChanges)

			// The update must store a different snapshot exactly when the preview reports a state change.
			p.Options.PreviewChanges = nil
			persister := &backend.ValidatingPersister{}
			update := lt.TestOp(func(
				info UpdateInfo, ctx *Context, opts UpdateOptions, dryRun bool,
			) (*deploy.Plan, display.ResourceChanges, error) {
				manager := backend.NewSnapshotManager(persister, b64.NewBase64SecretsManager(), info.Target.Snapshot, nil)
				ctx.SnapshotManager = &CombinedManager{Managers: []SnapshotManager{ctx.SnapshotManager, manager}}
				plan, changes, err := Update(info, ctx, opts, dryRun)
				return plan, changes, errors.Join(err, manager.Close())
			})
			_, err = update.RunStep(p.GetProject(), p.GetTarget(t, snap), p.Options, false, p.BackendClient, nil, "3")
			require.NoError(t, err)
			require.NotNil(t, persister.Snap)
			stored := !assert.ObjectsAreEqual(storedState(base), storedState(persister.Snap))
			assert.Equal(t, stored, previewChanges.State)
		})
	}
}

// storedState returns the parts of deployment that PreviewChanges.State covers. The order of dependencies carries no
// meaning, and a property with an empty dependency list has no dependencies.
func storedState(deployment *apitype.DeploymentV3) apitype.DeploymentV3 {
	state := *deployment
	state.Manifest = apitype.ManifestV1{}
	state.Resources = make([]apitype.ResourceV3, len(deployment.Resources))
	for i, res := range deployment.Resources {
		res.SourcePosition = ""
		res.StackTrace = nil
		res.InitErrors = nil
		res.Dependencies = slices.Sorted(slices.Values(res.Dependencies))
		propertyDependencies := map[resource.PropertyKey][]resource.URN{}
		for key, deps := range res.PropertyDependencies {
			if len(deps) > 0 {
				propertyDependencies[key] = slices.Sorted(slices.Values(deps))
			}
		}
		res.PropertyDependencies = propertyDependencies
		state.Resources[i] = res
	}
	return state
}
