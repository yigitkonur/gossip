package protocol

import (
	"encoding/json"
	"testing"
)

func TestEnvelope_ClassifyKind(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want EnvelopeKind
	}{
		{
			name: "response-to-client-request",
			raw:  `{"jsonrpc":"2.0","id":42,"result":{"ok":true}}`,
			want: KindResponse,
		},
		{
			name: "error-response",
			raw:  `{"jsonrpc":"2.0","id":42,"error":{"code":-32601,"message":"method not found"}}`,
			want: KindResponse,
		},
		{
			name: "notification",
			raw:  `{"jsonrpc":"2.0","method":"turn/started","params":{"turnId":"t1"}}`,
			want: KindNotification,
		},
		{
			name: "server-request",
			raw:  `{"jsonrpc":"2.0","id":7,"method":"item/permissions/requestApproval","params":{}}`,
			want: KindServerRequest,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var env Envelope
			if err := json.Unmarshal([]byte(c.raw), &env); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := env.Kind(); got != c.want {
				t.Errorf("Kind() = %v, want %v", got, c.want)
			}
		})
	}
}
