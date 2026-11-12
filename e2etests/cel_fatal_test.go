//go:build e2e

package e2etests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A broken CEL expression (syntax or evaluation error) must fail the whole lint run with exit
// code 2, be reported exactly once naming the check (and, for evaluation errors, the object),
// and never be counted as a lint diagnostic. Valid expressions keep the historical output and
// exit codes (0 when clean, 1 with findings).
func TestCELErrorsFailTheRunWithExitCodeTwo(t *testing.T) {
	kubeLinterBin := os.Getenv(kubeLinterBinEnv)
	require.NotEmpty(t, kubeLinterBin, "Please set %s", kubeLinterBinEnv)
	_, err := os.Stat(kubeLinterBin)
	require.NoError(t, err)

	td := filepath.Join("testdata", "cel-fatal")

	runLint := func(t *testing.T, config, manifest string) (string, int) {
		t.Helper()
		out, runErr := exec.Command(kubeLinterBin, "lint",
			"--config", filepath.Join(td, config),
			"--do-not-auto-add-defaults",
			filepath.Join(td, manifest)).CombinedOutput()
		if runErr == nil {
			return string(out), 0
		}
		exitErr, ok := runErr.(*exec.ExitError)
		require.True(t, ok, "unexpected error type: %v", runErr)
		return string(out), exitErr.ExitCode()
	}

	t.Run("valid expression with findings keeps exit code 1 and wording", func(t *testing.T) {
		out, code := runLint(t, "good-config.yaml", "deployments.yaml")
		assert.Equal(t, 1, code, "output: %s", out)
		assert.Contains(t, out, "CEL check expression returned: too many replicas")
		assert.Contains(t, out, "found 1 lint errors")
	})

	t.Run("valid expression without findings keeps exit code 0 and message", func(t *testing.T) {
		out, code := runLint(t, "good-pod-config.yaml", "deployments.yaml")
		assert.Equal(t, 0, code, "output: %s", out)
		assert.Contains(t, out, "No lint errors found!")
	})

	t.Run("syntax error fails even when no object matches the scope", func(t *testing.T) {
		out, code := runLint(t, "bad-syntax-config.yaml", "only-pods.yaml")
		assert.Equal(t, 2, code, "output: %s", out)
		assert.Contains(t, out, `check "cel-broken-syntax" failed`)
		assert.Contains(t, out, "failed to compile CEL expression")
		assert.NotContains(t, out, "No lint errors found!")
	})

	t.Run("syntax error with matching objects is reported once, not as a lint error", func(t *testing.T) {
		out, code := runLint(t, "bad-syntax-config.yaml", "deployments.yaml")
		assert.Equal(t, 2, code, "output: %s", out)
		assert.Equal(t, 1, strings.Count(out, "failed to compile CEL expression"))
		assert.NotContains(t, out, "lint errors")
	})

	t.Run("evaluation error names the check and the object and aborts the run", func(t *testing.T) {
		out, code := runLint(t, "bad-eval-config.yaml", "deployment-no-replicas.yaml")
		assert.Equal(t, 2, code, "output: %s", out)
		assert.Contains(t, out, `check "cel-missing-field" failed on object <no namespace>/no-replicas`)
		assert.Contains(t, out, "failed to evaluate CEL expression: no such key: replicas")
		assert.NotContains(t, out, "lint errors")
	})

	t.Run("the same broken expression is reported once across many objects", func(t *testing.T) {
		out, code := runLint(t, "bad-eval-config.yaml", "mixed.yaml")
		assert.Equal(t, 2, code, "output: %s", out)
		assert.Equal(t, 1, strings.Count(out, "failed to evaluate CEL expression"))
		assert.Equal(t, 1, strings.Count(out, "missing-replicas"))
	})
}
