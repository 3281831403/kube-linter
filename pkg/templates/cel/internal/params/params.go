package params

import (
	"github.com/google/cel-go/cel"
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
)

// Params defines the configuration parameters for this template.
type Params struct {
	// Check contains a CEL expression for validation logic. Two predefined variables are available: 'object' (the current Kubernetes object being processed) and 'objects' (all objects being linted).
	// +required
	// +noregex
	Check string

	// program is the compiled form of Check, built once when the check is instantiated and
	// reused for every linted object. It is not populated from (and not exposed to) config.
	program cel.Program

	// runtime holds the state of the check across the objects of a single lint run, including
	// the first fatal evaluation error. The pointer is allocated when the check parameters are
	// parsed, so every copy of Params (including the one captured by the check closure) shares
	// the same state.
	runtime *runtimeState
}

// runtimeState carries the state of a CEL check across the objects of a single lint run.
type runtimeState struct {
	// err is the first fatal evaluation error, if any.
	err error
	// objectMaps caches the JSON-derived map representation of the objects of each lint
	// context (keyed by the context's object set). Without it, the 'objects' list would be
	// re-marshaled once per evaluated object, turning the run into O(n^2) marshaling work.
	objectMaps map[objectsKey][]map[string]any
}

// objectsKey identifies the set of objects of a lint context without relying on the context
// implementation being comparable: the backing K8sObjects are pointers (hence comparable) and
// the length disambiguates distinct slices that happen to share a first element.
type objectsKey struct {
	first k8sutil.Object
	len   int
}

// ParseAndValidateParams parses and validates the template parameters and allocates the
// per-run runtime state shared by the check closure and the instantiated check.
func ParseAndValidateParams(m map[string]interface{}) (interface{}, error) {
	parsed, err := ParseAndValidate(m)
	if err != nil {
		return nil, err
	}
	p := parsed.(Params)
	p.runtime = &runtimeState{objectMaps: make(map[objectsKey][]map[string]any)}
	return p, nil
}

// WithProgram returns a copy of p configured with a compiled program. The expression is
// compiled once, when the check is instantiated, and the returned program is reused for every
// linted object. The runtime state pointer is preserved.
func WithProgram(p Params, program cel.Program) Params {
	p.program = program
	return p
}

// Program returns the compiled CEL program backing the check.
func (p Params) Program() cel.Program {
	return p.program
}

// RecordRuntimeError stores err as the first fatal runtime error encountered while evaluating
// the expression. Subsequent errors are ignored so that a broken expression is reported
// exactly once regardless of the number of objects.
func (p Params) RecordRuntimeError(err error) {
	if p.runtime != nil && p.runtime.err == nil {
		p.runtime.err = err
	}
}

// ObjectsKey derives the cache key for the objects of a lint context.
func ObjectsKey(objects []lintcontext.Object) objectsKey {
	key := objectsKey{len: len(objects)}
	if len(objects) > 0 {
		key.first = objects[0].K8sObject
	}
	return key
}

// CachedObjectsMaps returns the cached map representation of the objects identified by key,
// together with whether a cache entry exists.
func (p Params) CachedObjectsMaps(key objectsKey) ([]map[string]any, bool) {
	if p.runtime == nil {
		return nil, false
	}
	maps, ok := p.runtime.objectMaps[key]
	return maps, ok
}

// CacheObjectsMaps stores the map representation of the objects identified by key for reuse
// by later evaluations of the same check within the same lint run.
func (p Params) CacheObjectsMaps(key objectsKey, objectMaps []map[string]any) {
	if p.runtime != nil {
		p.runtime.objectMaps[key] = objectMaps
	}
}

// FatalError returns the first fatal error encountered while evaluating the expression, or
// nil if the expression evaluated without errors. It implements check.FatalErrorProvider.
func (p Params) FatalError() error {
	if p.runtime == nil {
		return nil
	}
	return p.runtime.err
}
