package pi

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompileMCPSanitizesNativeServerNameLikeOtherHarnesses(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "math.api", Namespace: "test"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
			URL:      "https://mcp.example.com/mcp",
		},
	}
	compilation, err := (&Compiler{}).compileMCP(context.Background(), "test", []v2translator.ResolvedMCPTool{{Server: server}})
	require.NoError(t, err)
	require.Len(t, compilation.servers, 1)
	require.Equal(t, "math_api", compilation.servers[0].Name)
}

func TestCompileMCPRejectsNativeNameCollision(t *testing.T) {
	first := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "math.api", Namespace: "test"},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://one.example.com/mcp"},
	}
	second := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "math_api", Namespace: "test"},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://two.example.com/mcp"},
	}
	_, err := (&Compiler{}).compileMCP(context.Background(), "test", []v2translator.ResolvedMCPTool{{Server: first}, {Server: second}})
	require.ErrorContains(t, err, "map to the same native namespace")
}
