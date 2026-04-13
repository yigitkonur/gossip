package protocol

import "testing"

// TestProtocolTypes_Compile asserts that the protocol package still exposes the method constants we depend on.
func TestProtocolTypes_Compile(t *testing.T) {
	_ = MethodThreadStart
	_ = MethodTurnStart
	_ = MethodItemAgentMessageDelta
	_ = MethodItemPermissionsRequestApproval
}
