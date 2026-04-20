package protocol

import "testing"

func TestMethods_NotificationLookup(t *testing.T) {
	if !IsNotificationMethod(MethodItemAgentMessageDelta) {
		t.Errorf("%s should be classified as notification", MethodItemAgentMessageDelta)
	}
	if IsNotificationMethod(MethodTurnStart) {
		t.Errorf("%s is a request, not a notification", MethodTurnStart)
	}
}

func TestMethods_ServerRequestLookup(t *testing.T) {
	if !IsServerRequestMethod(MethodItemPermissionsRequestApproval) {
		t.Errorf("%s should be classified as server request", MethodItemPermissionsRequestApproval)
	}
	if IsServerRequestMethod(MethodTurnCompleted) {
		t.Errorf("%s is a notification, not a server request", MethodTurnCompleted)
	}
}
