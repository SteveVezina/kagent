package e2e_test

import (
	"bytes"
	"context"
	"embed"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockmcp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed mocks/invoke_pi_mcp.json
var piMCPMocks embed.FS

func TestE2EPiMockWholeServerMCP(t *testing.T) {
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)
	kube := interactionKubeClient(t)
	mcpServer := createPiMCPServer(t, kube, mcpURL, nil)
	modelURL := reachableModelURL(t, startMockLLMServer(t, piMCPMocks, "mocks/invoke_pi_mcp.json"))
	model := createPiMockModel(t, kube, modelURL)
	template := createPiMCPTemplate(t, kube, model.Name, mcpServer.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)

	streamed := sendPiToolStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "PI_MCP_DONE result is 8") {
		t.Fatalf("Pi MCP task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertPiMCPCallReachedServer(t, mcpMock.Requests())
	assertPiToolEvents(t, streamed.toolEvents, "add_numbers")
	assertPiToolEvents(t, piTaskToolEvents(getPiTask(t, fixture, streamed.taskID)), "add_numbers")
}

func TestE2EPiMockSecretBackedMCPHeader(t *testing.T) {
	target := interactionTarget(t)
	mcpURL, mcpMock := startMCPMock(t)
	kube := interactionKubeClient(t)

	const secretHeader = "Bearer pi-mcp-secret"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "pi-mcp-auth-", Namespace: "kagent"},
		Data:       map[string][]byte{"token": []byte(secretHeader)},
	}
	if err := kube.Create(t.Context(), secret); err != nil {
		t.Fatalf("create Pi MCP auth Secret: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), secret); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Pi MCP auth Secret: %v", err)
		}
	})

	headers := []v1alpha3.ValueRef{{
		Name: "Authorization",
		ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource,
			Name: secret.Name,
			Key:  "token",
		},
	}}
	mcpServer := createPiMCPServer(t, kube, mcpURL, headers)
	modelURL := reachableModelURL(t, startMockLLMServer(t, piMCPMocks, "mocks/invoke_pi_mcp.json"))
	model := createPiMockModel(t, kube, modelURL)
	template := createPiMCPTemplate(t, kube, model.Name, mcpServer.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)

	streamed := sendPiToolStreaming(t, fixture, "Add 3 and 5 using the configured MCP server.")
	if streamed.state != a2atype.TaskStateCompleted || !strings.Contains(streamed.text, "PI_MCP_DONE result is 8") {
		t.Fatalf("Pi secret-header MCP task state = %s, text = %q, failure = %q", streamed.state, streamed.text, streamed.failureText)
	}
	assertPiMCPCallReachedServer(t, mcpMock.Requests())
	assertPiMCPHeader(t, mcpMock.Requests(), "Authorization", secretHeader)
	assertPiToolEvents(t, streamed.toolEvents, "add_numbers")
}

func createPiMCPServer(t *testing.T, kube ctrlclient.Client, mcpURL string, headers []v1alpha3.ValueRef) *v1alpha3.RemoteMCPServer {
	t.Helper()
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "pi-resources-", Namespace: "kagent"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Description: "Pi whole-server MCP E2E fixture",
			Protocol:    v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:         mcpURL,
			HeadersFrom: headers,
		},
	}
	if err := kube.Create(t.Context(), server); err != nil {
		t.Fatalf("create Pi RemoteMCPServer: %v", err)
	}
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), server); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete Pi RemoteMCPServer: %v", err)
		}
	})
	return server
}

func createPiMCPTemplate(t *testing.T, kube ctrlclient.Client, modelConfig, mcpServer string) string {
	t.Helper()
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pi-resources-", Namespace: "kagent",
			Labels: map[string]string{"kagent.dev/e2e-runtime": "pi"},
		},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: modelConfig},
			Description:  "Pi direct whole-server MCP E2E fixture",
			SystemPrompt: "Use the configured MCP tool. Do not calculate the answer yourself.",
			Tools: []v1alpha3.ToolBinding{{MCP: &v1alpha3.MCPToolBinding{
				Server: corev1.TypedLocalObjectReference{Kind: "RemoteMCPServer", Name: mcpServer},
			}}},
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, piE2EHarness)
	return template.Name
}

func assertPiMCPCallReachedServer(t *testing.T, requests []mockmcp.RecordedRequest) {
	t.Helper()
	for _, request := range requests {
		if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && bytes.Contains(request.Body, []byte(`"name":"add_numbers"`)) {
			return
		}
	}
	t.Fatal("mock MCP server did not receive an add_numbers tools/call request")
}

func assertPiMCPHeader(t *testing.T, requests []mockmcp.RecordedRequest, name, value string) {
	t.Helper()
	for _, request := range requests {
		if bytes.Contains(request.Body, []byte(`"method":"tools/call"`)) && request.Headers.Get(name) == value {
			return
		}
	}
	t.Fatalf("mock MCP tools/call request did not contain %s=%q", name, value)
}
