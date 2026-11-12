package cel

import (
	"encoding/json"
	"fmt"

	"github.com/google/cel-go/cel"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"golang.stackrox.io/kube-linter/pkg/templates"
	"golang.stackrox.io/kube-linter/pkg/templates/cel/internal/params"
)

const (
	templateKey = "cel-expression"
)

func init() {
	templates.Register(check.Template{
		HumanName:   "CEL",
		Key:         templateKey,
		Description: "Flag objects with CEL expression",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{objectkinds.Any},
		},
		Parameters:             params.ParamDescs,
		ParseAndValidateParams: params.ParseAndValidateParams,
		Instantiate: params.WrapInstantiateFunc(func(p params.Params) (check.Func, error) {
			// Compile the expression once, when the check is loaded. The resulting program is
			// reused for every linted object instead of being rebuilt per object. A syntax or
			// type error therefore fails the check (and the whole lint run) even when no object
			// matches the check's scope.
			program, err := compile(p.Check)
			if err != nil {
				return nil, &check.ExecutionError{Err: err}
			}
			p = params.WithProgram(p, program)
			return func(ctx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
				msg, err := evaluate(p, ctx, object)
				if err != nil {
					// A broken expression is not a lint finding. Record it once, naming the
					// object it failed on, and let the lint runner abort the run instead of
					// emitting a per-object diagnostic.
					p.RecordRuntimeError(&check.ExecutionError{
						ObjectName: object.GetK8sObjectName().String(),
						Err:        err,
					})
					return nil
				}
				if msg != "" {
					return []diagnostic.Diagnostic{
						{Message: fmt.Sprintf("CEL check expression returned: %v", msg)},
					}
				}
				return nil
			}, nil
		}),
	})
}

// compile builds the CEL environment, compiles the given expression and turns it into a
// reusable program. The available variables ('object' and 'objects') are part of the
// environment and are therefore identical for every evaluation of the program.
func compile(expression string) (cel.Program, error) {
	e, err := cel.NewEnv(
		cel.Variable("object", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("objects", cel.ListType(cel.MapType(cel.StringType, cel.AnyType))),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	ast, iss := e.Compile(expression)
	if iss.Err() != nil {
		return nil, fmt.Errorf("failed to compile CEL expression: %w", iss.Err())
	}
	program, err := e.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL program: %w", err)
	}
	return program, nil
}

func evaluate(p params.Params, ctx lintcontext.LintContext, object lintcontext.Object) (string, error) {
	// Convert the current object to a map via JSON marshaling/unmarshaling for CEL
	// compatibility. We need to marshal the underlying K8sObject, not the lintcontext.Object.
	objectMap, err := toMap(object.K8sObject)
	if err != nil {
		return "", fmt.Errorf("failed to convert object to map: %w", err)
	}

	// The 'objects' list is identical for every object of a given lint context, so its map
	// representation is computed once per context and reused (the compiled expression itself
	// is already reused across all objects).
	objects := ctx.Objects()
	key := params.ObjectsKey(objects)
	objectsMaps, cached := p.CachedObjectsMaps(key)
	if !cached {
		objectsMaps = make([]map[string]any, len(objects))
		for i, obj := range objects {
			objMap, err := toMap(obj.K8sObject)
			if err != nil {
				return "", fmt.Errorf("failed to convert object %s to map: %w", obj.GetK8sObjectName().String(), err)
			}
			objectsMaps[i] = objMap
		}
		p.CacheObjectsMaps(key, objectsMaps)
	}

	out, _, err := p.Program().Eval(map[string]any{
		"object":  objectMap,
		"objects": objectsMaps,
	})
	if err != nil {
		return "", fmt.Errorf("failed to evaluate CEL expression: %w", err)
	}

	o, ok := out.Value().(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %v", out.Value())
	}
	return o, nil
}

func toMap(obj any) (map[string]any, error) {
	bytes, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal object: %w", err)
	}
	var output map[string]any
	if err := json.Unmarshal(bytes, &output); err != nil {
		return nil, fmt.Errorf("failed to unmarshal object: %w", err)
	}
	return output, nil
}
