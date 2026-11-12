package run

import (
	"errors"
	"fmt"
	"time"

	"golang.stackrox.io/kube-linter/internal/version"
	checkpkg "golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/checkregistry"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/ignore"
	"golang.stackrox.io/kube-linter/pkg/instantiatedcheck"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
)

// CheckStatus is enum type.
type CheckStatus string

const (
	// ChecksPassed means no lint errors found.
	ChecksPassed CheckStatus = "Passed"
	// ChecksFailed means lint errors were found.
	ChecksFailed CheckStatus = "Failed"
)

// Result represents the result from a run of the linter.
type Result struct {
	Checks  []config.Check
	Reports []diagnostic.WithContext
	Summary Summary
}

// Summary holds information about the linter run overall.
type Summary struct {
	ChecksStatus      CheckStatus
	CheckEndTime      time.Time
	KubeLinterVersion string
}

// Run runs the linter on the given context, with the given config.
func Run(lintCtxs []lintcontext.LintContext, registry checkregistry.CheckRegistry, checks []string) (Result, error) {
	var result Result

	instantiatedChecks := make([]*instantiatedcheck.InstantiatedCheck, 0, len(checks))
	for _, checkName := range checks {
		instantiatedCheck := registry.Load(checkName)
		if instantiatedCheck == nil {
			return Result{}, fmt.Errorf("check %q not found", checkName)
		}
		instantiatedChecks = append(instantiatedChecks, instantiatedCheck)
		result.Checks = append(result.Checks, instantiatedCheck.Spec)
	}

	for _, lintCtx := range lintCtxs {
		for _, obj := range lintCtx.Objects() {
			for _, check := range instantiatedChecks {
				if !check.Matcher.Matches(obj.K8sObject.GetObjectKind().GroupVersionKind()) {
					continue
				}
				if ignore.ObjectForCheck(obj.K8sObject.GetAnnotations(), check.Spec.Name) {
					continue
				}
				diagnostics := check.Func(lintCtx, obj)
				if check.FatalError != nil {
					if err := check.FatalError(); err != nil {
						// A fatal check failure (e.g. a broken CEL expression) invalidates the
						// whole lint run: it is not a lint finding, so it must not be counted in
						// result.Reports. Name the check that failed and abort immediately.
						var execErr *checkpkg.ExecutionError
						if errors.As(err, &execErr) {
							execErr.CheckName = check.Spec.Name
							return Result{}, execErr
						}
						return Result{}, &checkpkg.ExecutionError{
							CheckName: check.Spec.Name,
							Err:       err,
						}
					}
				}
				for _, d := range diagnostics {
					result.Reports = append(result.Reports, diagnostic.WithContext{
						Diagnostic:  d,
						Check:       check.Spec.Name,
						Remediation: check.Spec.Remediation,
						Object:      obj,
					})
				}
			}
		}
	}

	if len(result.Reports) > 0 {
		result.Summary.ChecksStatus = ChecksFailed
	} else {
		result.Summary.ChecksStatus = ChecksPassed
	}
	result.Summary.CheckEndTime = time.Now().UTC()
	result.Summary.KubeLinterVersion = version.Get()

	return result, nil
}
