package translator_test

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompilerRoutesPiHarness(t *testing.T) {
	collections := mockCollections(t, modelConfig())
	adapter := &testHarnessCompiler{}
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Name: "pi", Namespace: "test"},
		Spec: v1alpha3.HarnessSpec{
			Pi:                    &v1alpha3.PiHarness{},
			AllowedAgentTemplates: &v1alpha3.HarnessAgentTemplateAdmission{Selector: metav1.LabelSelector{}},
		},
	}
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "test"},
		Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "default-model"}},
	}

	revision, err := v2translator.NewCompiler(krt.TestingDummyContext{}, collections, map[v2translator.HarnessType]v2translator.HarnessCompiler{
		v2translator.HarnessTypePi: adapter,
	}).CompileAgentTemplate(context.Background(), harness, template)
	require.NoError(t, err)
	require.Equal(t, "assistant", revision.AgentTemplateName)
	require.NotNil(t, adapter.input)
	require.Equal(t, template.Name, adapter.input.Root.Template.Name)
}
