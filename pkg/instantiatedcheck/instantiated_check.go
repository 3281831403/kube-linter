package instantiatedcheck

import (
	"errors"
	"fmt"
	"regexp"

	"golang.stackrox.io/kube-linter/internal/errorhelpers"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"golang.stackrox.io/kube-linter/pkg/templates"
)

// An InstantiatedCheck is the runtime instantiation of a check, which fuses the metadata in a check
// spec with the runtime information from a template.
type InstantiatedCheck struct {
	Func    check.Func
	Matcher objectkinds.Matcher

	// FatalError, if non-nil, returns a fatal error (expected to wrap check.ExecutionError)
	// collected while running Func across objects. It is implemented by checks whose failures
	// invalidate the whole lint run rather than producing a diagnostic per object.
	FatalError func() error

	Spec config.Check
}

var (
	validCheckNameRegex = regexp.MustCompile(`^[a-zA-Z0-9-_]+$`)
)

// ValidateAndInstantiate validates the check, and creates an instantiated check if the check
// is valid.
func ValidateAndInstantiate(c *config.Check) (*InstantiatedCheck, error) {
	validationErrs := errorhelpers.NewErrorList("validating check")
	if c.Name == "" {
		validationErrs.AddString("no name specified")
	}
	if !validCheckNameRegex.MatchString(c.Name) {
		validationErrs.AddStringf("invalid name %s, must match regex %s", c.Name, validCheckNameRegex.String())
	}
	template, found := templates.Get(c.Template)
	if !found {
		validationErrs.AddStringf("template %q not found", c.Template)
		return nil, validationErrs.ToError()
	}

	params, err := template.ParseAndValidateParams(c.Params)
	if err != nil {
		return nil, fmt.Errorf("validating and instantiating params: %w", err)
	}

	if err := validationErrs.ToError(); err != nil {
		return nil, err
	}

	i := &InstantiatedCheck{Spec: *c}
	var objectKinds config.ObjectKindsDesc
	if c.Scope != nil {
		objectKinds = *c.Scope
	} else {
		objectKinds = template.SupportedObjectKinds
	}
	matcher, err := objectkinds.ConstructMatcher(objectKinds.ObjectKinds...)
	if err != nil {
		return nil, err
	}
	i.Matcher = matcher
	checkFunc, err := template.Instantiate(params)
	if err != nil {
		// Preserve fatal execution errors (e.g. a CEL expression that fails to compile) as a
		// recognizable type so they can fail the whole run with a distinct exit code, even
		// when no object matches the check's scope.
		var execErr *check.ExecutionError
		if errors.As(err, &execErr) {
			return nil, err
		}
		return nil, fmt.Errorf("instantiating check: %w", err)
	}
	i.Func = checkFunc
	// Checks that can fail fatally (independently of the diagnostics they emit) expose their
	// collected error through check.FatalErrorProvider. Wire it up when the parsed parameters
	// implement it; ordinary check templates are unaffected.
	if fatalErrorProvider, ok := params.(check.FatalErrorProvider); ok {
		i.FatalError = fatalErrorProvider.FatalError
	}
	return i, nil
}
