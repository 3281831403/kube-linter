package cel

import (
	"fmt"
	"testing"

	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeObjects(n int) ([]lintcontext.Object, lintcontext.LintContext) {
	objs := make([]lintcontext.Object, n)
	for i := 0; i < n; i++ {
		objs[i] = lintcontext.Object{K8sObject: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("deploy-%d", i)},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(int32(i % 5))},
		}}
	}
	return objs, stubLintContext{objects: objs}
}

func int32Ptr(v int32) *int32 { return &v }

func BenchmarkCELCheck(b *testing.B) {
	template, found := templates.Get(templateKey)
	if !found {
		b.Fatal("template not found")
	}
	for _, n := range []int{100, 300, 1000} {
		b.Run(fmt.Sprintf("objects=%d", n), func(b *testing.B) {
			objs, ctx := makeObjects(n)
			parsed, err := template.ParseAndValidateParams(map[string]interface{}{
				"check": `size(objects) > 0 && object.spec.replicas > 2 ? "high" : ""`,
			})
			if err != nil {
				b.Fatal(err)
			}
			fn, err := template.Instantiate(parsed)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, obj := range objs {
					fn(ctx, obj)
				}
			}
		})
	}
}
