package pi

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCompileMCPSortsServersByNativeName(t *testing.T) {
	servers := []*v1alpha3.RemoteMCPServer{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "zeta", Namespace: "test"},
			Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://zeta.example.com/mcp"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "test"},
			Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://alpha.example.com/mcp"},
		},
	}

	compilation, err := (&Compiler{}).compileMCP(context.Background(), "test", []v2translator.ResolvedMCPTool{
		{Server: servers[0]},
		{Server: servers[1]},
	})
	require.NoError(t, err)
	require.Len(t, compilation.servers, 2)
	require.Equal(t, "alpha", compilation.servers[0].Name)
	require.Equal(t, "zeta", compilation.servers[1].Name)
}
