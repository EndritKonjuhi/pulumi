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

package ints

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulumi/pulumi/tests/testutil"

	ptesting "github.com/pulumi/pulumi/sdk/v3/go/common/testing"
)

func TestExitCode_StackNotFound(t *testing.T) {
	t.Parallel()

	e := ptesting.NewEnvironment(t)
	defer e.DeleteIfNotFailed()

	// Create a minimal project so the DIY file backend can infer the
	// current project name when parsing stack references.
	e.WriteTestFile("Pulumi.yaml", `
name: exit-code-stack-not-found
runtime: nodejs
`)

	// Use a local file backend so we don't depend on external services.
	e.Backend = e.LocalURL()

	cmd := e.SetupCommandIn(t.Context(), e.CWD, "pulumi", "stack", "select", "does-not-exist")
	err := cmd.Run()

	require.Error(t, err) //nolint:forbidigo // pre-existing
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok)
	require.Equal(t, 6, exitErr.ExitCode())
}

// TestExitCode_AwaitError verifies that a deployment whose only failure is a provider AwaitError
// exits with the dedicated ExitAwaitError code (10) rather than the generic error code (1).
//
// other tests that mutate PULUMI_HOME could clash on the shared plugin cache.
//
//nolint:paralleltest // NewEnvironment already installs a per-test tempdir; running in parallel with
func TestExitCode_AwaitError(t *testing.T) {
	e := ptesting.NewEnvironment(t)
	defer e.DeleteIfNotFailed()

	providerDir := testutil.TestProviderDir(t)
	pulumiYaml := fmt.Sprintf(`name: exit-code-await
runtime: nodejs
plugins:
  providers:
    - name: testprovider
      path: %s
`, providerDir)
	require.NoError(t, os.WriteFile(filepath.Join(e.CWD, "Pulumi.yaml"), []byte(pulumiYaml), 0o600))
	e.WriteTestFile("package.json", `{"name": "exit-code-await", "main": "index.js"}`)
	e.WriteTestFile("index.js", `const pulumi = require("@pulumi/pulumi");
new pulumi.CustomResource("testprovider:index:AwaitsOnCreate", "awaiting", {});
`)

	e.Backend = e.LocalURL()
	e.Env = append(e.Env, "PULUMI_CONFIG_PASSPHRASE=")
	e.RunCommand("pulumi", "login", "--cloud-url", e.LocalURL())
	e.RunCommand("pulumi", "stack", "init", "test", "--secrets-provider", "passphrase")
	e.InstallDependencies()

	cmd := e.SetupCommandIn(t.Context(), e.CWD, "pulumi", "up", "--yes", "--skip-preview")
	err := cmd.Run()

	require.Error(t, err) //nolint:forbidigo // pre-existing
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "expected pulumi up to return an *exec.ExitError, got %T: %v", err, err)
	require.Equal(t, 10, exitErr.ExitCode(), "await failure should exit with ExitAwaitError (10)")
}

// TestExitCode_PreviewDetailedExitCode verifies the exit codes of `pulumi preview --detailed-exitcode`.
func TestExitCode_PreviewDetailedExitCode(t *testing.T) {
	t.Parallel()

	e := ptesting.NewEnvironment(t)
	defer e.DeleteIfNotFailed()

	programDir, err := filepath.Abs("preview_detailed_exitcode")
	require.NoError(t, err)
	pulumiYaml := fmt.Sprintf(`name: exit-code-preview
runtime: go
main: %s
`, programDir)
	require.NoError(t, os.WriteFile(filepath.Join(e.CWD, "Pulumi.yaml"), []byte(pulumiYaml), 0o600))

	e.Backend = e.LocalURL()
	e.Env = append(e.Env, "PULUMI_CONFIG_PASSPHRASE=")
	e.RunCommand("pulumi", "login", "--cloud-url", e.LocalURL())
	e.RunCommand("pulumi", "stack", "init", "test", "--secrets-provider", "passphrase")
	e.RunCommand("pulumi", "up", "--yes", "--skip-preview")

	preview := func(env []string, args ...string) int {
		cmd := e.SetupCommandIn(t.Context(), e.CWD, "pulumi",
			append([]string{"preview", "--detailed-exitcode"}, args...)...)
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0
		}
		exitErr, ok := err.(*exec.ExitError)
		require.True(t, ok, "expected pulumi preview to return an *exec.ExitError, got %T: %v\n%s", err, err, out)
		return exitErr.ExitCode()
	}

	assert.Equal(t, 0, preview(nil), "nothing changes")
	assert.Equal(t, 11, preview([]string{"EXTRA_RESOURCE=1", "STACK_OUTPUT=new", "COMPONENT_OUTPUT=new"}),
		"resources change")
	assert.Equal(t, 12, preview([]string{"STACK_OUTPUT=new", "COMPONENT_OUTPUT=new"}), "stack outputs change")
	assert.Equal(t, 13, preview([]string{"COMPONENT_OUTPUT=new"}), "only the state changes")
	assert.Equal(t, 7, preview([]string{"EXTRA_RESOURCE=1"}, "--expect-no-changes"),
		"--expect-no-changes takes precedence")

	plan := filepath.Join(e.CWD, "plan.json")
	assert.Equal(t, 11, preview([]string{"EXTRA_RESOURCE=1"}, "--save-plan", plan), "resources change with a plan")
	assert.FileExists(t, plan)
}
