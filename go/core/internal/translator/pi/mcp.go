package pi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	piconfig "github.com/kagent-dev/kagent/go/harness/pi/config"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type mcpCompilation struct {
	servers     []piconfig.MCPServer
	environment []corev1.EnvVar
	egress      []string
}

func (c *Compiler) compileMCP(ctx context.Context, namespace string, tools []v2translator.ResolvedMCPTool) (mcpCompilation, error) {
	if len(tools) == 0 {
		return mcpCompilation{}, nil
	}
	result := mcpCompilation{servers: make([]piconfig.MCPServer, 0, len(tools))}
	seenServers := map[string]struct{}{}
	nativeNames := map[string]string{}
	for _, tool := range tools {
		server := tool.Server
		if server == nil {
			return mcpCompilation{}, fmt.Errorf("resolved Pi MCP binding has no server")
		}
		if strings.TrimSpace(server.Name) == "" {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer name is required")
		}
		if _, exists := seenServers[server.Name]; exists {
			return mcpCompilation{}, v2translator.NewValidationError("RemoteMCPServer %q is bound more than once", server.Name)
		}
		seenServers[server.Name] = struct{}{}
		nativeName := strings.ReplaceAll(server.Name, "-", "_")
		if previous, exists := nativeNames[nativeName]; exists {
			return mcpCompilation{}, v2translator.NewValidationError("Pi MCP servers %q and %q map to the same native namespace %q", previous, server.Name, nativeName)
		}
		nativeNames[nativeName] = server.Name

		if server.Spec.Protocol != "" && server.Spec.Protocol != v1alpha3.RemoteMCPServerProtocolStreamableHttp {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q requires Streamable HTTP", server.Name)
		}
		if server.Spec.SseReadTimeout != nil {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q does not support sseReadTimeout", server.Name)
		}
		if !server.Spec.TLS.IsEmpty() {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q does not support custom TLS configuration", server.Name)
		}
		if server.Spec.TerminateOnClose != nil && !*server.Spec.TerminateOnClose {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q requires terminateOnClose=true", server.Name)
		}

		timeout := 30 * time.Second
		if server.Spec.Timeout != nil {
			timeout = server.Spec.Timeout.Duration
		}
		if timeout <= 0 {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q timeout must be positive", server.Name)
		}
		host, err := absoluteHTTPHostname(server.Spec.URL)
		if err != nil {
			return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q URL %v", server.Name, err)
		}
		headers, environment, err := c.compileMCPHeaders(ctx, namespace, server.Spec.HeadersFrom)
		if err != nil {
			return mcpCompilation{}, fmt.Errorf("compile RemoteMCPServer %q headers: %w", server.Name, err)
		}
		selected := append([]string(nil), tool.Binding.Tools...)
		for _, name := range selected {
			if strings.TrimSpace(name) == "" {
				return mcpCompilation{}, v2translator.NewValidationError("Pi RemoteMCPServer %q has an empty selected tool name", server.Name)
			}
		}
		slices.Sort(selected)
		selected = slices.Compact(selected)
		result.servers = append(result.servers, piconfig.MCPServer{
			Name: server.Name, URL: server.Spec.URL, Headers: headers, EnabledTools: selected, TimeoutSeconds: timeout.Seconds(),
		})
		result.environment = append(result.environment, environment...)
		result.egress = append(result.egress, host)
	}
	return result, nil
}

func (c *Compiler) compileMCPHeaders(ctx context.Context, namespace string, refs []v1alpha3.ValueRef) (map[string]string, []corev1.EnvVar, error) {
	if len(refs) == 0 {
		return nil, nil, nil
	}
	headers := make(map[string]string, len(refs))
	var environment []corev1.EnvVar
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" {
			return nil, nil, v2translator.NewValidationError("MCP header name is required")
		}
		if _, exists := headers[ref.Name]; exists {
			return nil, nil, v2translator.NewValidationError("duplicate MCP header %q", ref.Name)
		}
		switch {
		case ref.ValueFrom == nil:
			headers[ref.Name] = ref.Value
		case ref.ValueFrom.Type == v1alpha3.ConfigMapValueSource:
			configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: ref.ValueFrom.Name}))
			if configMap == nil {
				return nil, nil, fmt.Errorf("ConfigMap %q not found", ref.ValueFrom.Name)
			}
			value, ok := (*configMap).Data[ref.ValueFrom.Key]
			if !ok {
				return nil, nil, fmt.Errorf("ConfigMap %q does not contain key %q", ref.ValueFrom.Name, ref.ValueFrom.Key)
			}
			headers[ref.Name] = value
		case ref.ValueFrom.Type == v1alpha3.SecretValueSource:
			secret, err := c.secret(ctx, namespace, ref.ValueFrom.Name)
			if err != nil {
				return nil, nil, err
			}
			if len(secret.Data[ref.ValueFrom.Key]) == 0 {
				return nil, nil, v2translator.NewValidationError("Pi MCP credential Secret %q does not contain a non-empty key %q", ref.ValueFrom.Name, ref.ValueFrom.Key)
			}
			sum := sha256.Sum256([]byte(namespace + "\x00" + ref.ValueFrom.Name + "\x00" + ref.ValueFrom.Key))
			name := mcpCredentialPrefix + strings.ToUpper(fmt.Sprintf("%x", sum[:8]))
			headers[ref.Name] = "__KAGENT_ENV[" + name + "]__"
			environment = append(environment, secretEnvironment(name, ref.ValueFrom.Name, ref.ValueFrom.Key))
		default:
			return nil, nil, v2translator.NewValidationError("unsupported MCP header value source %q", ref.ValueFrom.Type)
		}
	}
	return headers, environment, nil
}
