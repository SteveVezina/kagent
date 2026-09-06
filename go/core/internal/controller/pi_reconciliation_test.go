package controller

import (
	"testing"

	"github.com/agent-substrate/substrate/pkg/api/ate/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPiReconciliationCompilesActorTemplate(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test-pi", nil)
	chat := kagentv1alpha3.OpenAIAPIFormatChatCompletions
	template := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid", Labels: map[string]string{"runtime": "pi"}},
		Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "help"},
	}
	piHarness := harness("team-a", "pi", map[string]string{"runtime": "pi"})
	piHarness.UID = "harness-uid"
	piHarness.Spec.Pi = &kagentv1alpha3.PiHarness{}
	piHarness.Spec.Workload.Image = "example.com/pi@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	piHarness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{
		WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
	}
	model := &kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model", UID: "model-uid"}, Spec: kagentv1alpha3.ModelConfigSpec{
		Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5.1", APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
		OpenAI: &kagentv1alpha3.OpenAIConfig{APIFormat: &chat},
	}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model-auth", UID: "secret-uid"}, Data: map[string][]byte{"api-key": []byte("secret")}}
	mock := krttest.NewMock(t, []any{
		template,
		piHarness,
		model,
		secret,
		&v1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}},
	})
	templates := krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock)
	pairs := newPairCollection(templates, krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), opts)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	secrets := krttest.GetMockCollection[*corev1.Secret](mock)
	_, resolvedModelConfigs := newModelConfigReconciliations(krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock), configMaps, secrets, opts)
	reconciliations := newPairReconciliations(
		pairs, v2translator.Collections{
			AgentTemplates: templates, ResolvedModelConfigs: resolvedModelConfigs,
			RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
			ConfigMaps: configMaps, Secrets: secrets,
			WorkerPools: krttest.GetMockCollection[*v1alpha1.WorkerPool](mock),
		}, krttest.GetMockCollection[ObservedActorTemplate](mock), opts,
	)
	waitFor(t, func() bool {
		states := reconciliations.List()
		return len(states) == 1 && states[0].Failure == nil && states[0].DesiredActorTemplate != nil
	})
	state := reconciliations.List()[0]
	if state.Revision == nil || len(state.Revision.Environment) == 0 || state.Revision.Environment[0].Name != "OPENAI_API_KEY" || state.Revision.Environment[0].Value != "secret" {
		t.Fatalf("Pi revision environment = %#v", state.Revision)
	}
	if state.DesiredActorTemplate.GetContainers()[0].GetReadyz().GetHttpGet().GetPort() != 8081 {
		t.Fatalf("Pi ActorTemplate readiness = %#v", state.DesiredActorTemplate.GetContainers()[0].GetReadyz())
	}
}
