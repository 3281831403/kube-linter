package cel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/cel/internal/params"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompile(t *testing.T) {
	t.Run("valid expression compiles", func(t *testing.T) {
		program, err := compile(`object.metadata.name == "x" ? "yes" : ""`)
		require.NoError(t, err)
		require.NotNil(t, program)
	})

	t.Run("syntax error is reported at compile time", func(t *testing.T) {
		_, err := compile(`invalid syntax here`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to compile CEL expression")
	})
}

// instantiateCEL builds the check.Func and its parsed params exactly like check registration
// does, so tests exercise the same code path as a lint run.
func instantiateCEL(t *testing.T, expression string) (check.Func, params.Params) {
	t.Helper()
	template, found := templates.Get(templateKey)
	require.True(t, found)
	parsed, err := template.ParseAndValidateParams(map[string]interface{}{"check": expression})
	require.NoError(t, err)
	p := parsed.(params.Params)
	fn, err := template.Instantiate(parsed)
	require.NoError(t, err)
	return fn, p
}

func podObject(name string) lintcontext.Object {
	return lintcontext.Object{
		K8sObject: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		},
	}
}

type stubLintContext struct {
	objects []lintcontext.Object
}

func (s stubLintContext) Objects() []lintcontext.Object {
	return s.objects
}

func (s stubLintContext) InvalidObjects() []lintcontext.InvalidObject {
	return nil
}

func TestCheckFunc_ValidExpressions(t *testing.T) {
	tests := []struct {
		name            string
		expression      string
		object          lintcontext.Object
		objects         []lintcontext.Object
		expectedMessage string
	}{
		{
			name:            "simple string return",
			expression:      `"test message"`,
			object:          podObject("test-pod"),
			expectedMessage: "CEL check expression returned: test message",
		},
		{
			name:            "conditional message based on object",
			expression:      `object.metadata.name == "test-pod" ? "pod found" : "pod not found"`,
			object:          podObject("test-pod"),
			expectedMessage: "CEL check expression returned: pod found",
		},
		{
			name:       "check objects list length",
			expression: `size(objects) > 0 ? "objects found" : "no objects"`,
			object:     podObject("test-pod"),
			objects: []lintcontext.Object{
				{K8sObject: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "test-service"}}},
			},
			expectedMessage: "CEL check expression returned: objects found",
		},
		{
			name:       "complex expression with object properties",
			expression: `object.metadata.namespace == "default" && object.metadata.name.startsWith("test") ? "valid test object" : "invalid object"`,
			object: lintcontext.Object{K8sObject: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			}}},
			expectedMessage: "CEL check expression returned: valid test object",
		},
		{
			name:            "empty string return produces no diagnostic",
			expression:      `""`,
			object:          podObject("test-pod"),
			expectedMessage: "",
		},
		{
			name:       "accessing nested properties",
			expression: `object.metadata.labels.app == "web" ? "web app detected" : "not a web app"`,
			object: lintcontext.Object{K8sObject: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:   "test-pod",
				Labels: map[string]string{"app": "web"},
			}}},
			expectedMessage: "CEL check expression returned: web app detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, p := instantiateCEL(t, tt.expression)
			ctx := stubLintContext{objects: tt.objects}

			diagnostics := fn(ctx, tt.object)

			require.NoError(t, p.FatalError())
			if tt.expectedMessage == "" {
				assert.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, tt.expectedMessage, diagnostics[0].Message)
			}
		})
	}
}

func TestCheckFunc_SyntaxErrorFailsInstantiation(t *testing.T) {
	template, found := templates.Get(templateKey)
	require.True(t, found)
	parsed, err := template.ParseAndValidateParams(map[string]interface{}{"check": `invalid syntax here`})
	require.NoError(t, err)

	_, err = template.Instantiate(parsed)
	require.Error(t, err)
	var execErr *check.ExecutionError
	require.ErrorAs(t, err, &execErr)
	assert.Contains(t, err.Error(), "failed to compile CEL expression")
}

func TestCheckFunc_EvaluationErrorIsRecordedOnceAndNamesTheObject(t *testing.T) {
	// A Deployment without a replicas field: object.spec.replicas does not exist.
	makeDeployment := func(name string) lintcontext.Object {
		return lintcontext.Object{
			K8sObject: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: name},
			},
		}
	}
	objects := []lintcontext.Object{
		makeDeployment("deploy-1"),
		makeDeployment("deploy-2"),
		makeDeployment("deploy-3"),
	}
	fn, p := instantiateCEL(t, `object.spec.replicas > 1 ? "too many replicas" : ""`)
	ctx := stubLintContext{objects: objects}

	// A broken expression must never surface as a lint diagnostic, on any object.
	for _, obj := range objects {
		assert.Empty(t, fn(ctx, obj))
	}

	// The failure is reported exactly once, and names the object it first failed on.
	err := p.FatalError()
	require.Error(t, err)
	var execErr *check.ExecutionError
	require.ErrorAs(t, err, &execErr)
	assert.Contains(t, execErr.ObjectName, "deploy-1")
	assert.Contains(t, execErr.Error(), "failed to evaluate CEL expression")

	// Repeated evaluation keeps the first error and still yields no diagnostics.
	for _, obj := range objects {
		assert.Empty(t, fn(ctx, obj))
	}
	require.Same(t, err, p.FatalError())
}

func TestCheckFunc_NonStringOutputIsFatal(t *testing.T) {
	fn, p := instantiateCEL(t, `123`)
	ctx := stubLintContext{}

	assert.Empty(t, fn(ctx, podObject("test-pod")))

	err := p.FatalError()
	require.Error(t, err)
	var execErr *check.ExecutionError
	require.ErrorAs(t, err, &execErr)
	assert.Contains(t, execErr.ObjectName, "test-pod")
	assert.Contains(t, err.Error(), "expected string")
}

func TestCheckFunc_CompiledProgramIsReusedAcrossObjects(t *testing.T) {
	fn, p := instantiateCEL(t, `object.metadata.name.startsWith("shared-") ? "shared" : ""`)
	ctx := stubLintContext{objects: nil}

	for _, name := range []string{"shared-a", "shared-b", "other"} {
		diagnostics := fn(ctx, podObject(name))
		require.NoError(t, p.FatalError())
		if name == "other" {
			assert.Empty(t, diagnostics)
		} else {
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "CEL check expression returned: shared", diagnostics[0].Message)
		}
	}
}
