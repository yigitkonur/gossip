package protocol

// Client → server request methods.
const (
	MethodInitialize    = "initialize"
	MethodThreadStart   = "thread/start"
	MethodThreadResume  = "thread/resume"
	MethodThreadNameSet = "thread/name/set"
	MethodThreadList    = "thread/list"
	MethodReviewStart   = "review/start"
	MethodTurnStart     = "turn/start"
	MethodTurnInterrupt = "turn/interrupt"
)

// Server → client request methods (require a response).
const (
	MethodItemPermissionsRequestApproval      = "item/permissions/requestApproval"
	MethodItemFileChangeRequestApproval       = "item/fileChange/requestApproval"
	MethodItemCommandExecutionRequestApproval = "item/commandExecution/requestApproval"
)

// Server → client notification methods (no response).
const (
	MethodTurnStarted           = "turn/started"
	MethodTurnCompleted         = "turn/completed"
	MethodItemStarted           = "item/started"
	MethodItemAgentMessageDelta = "item/agentMessage/delta"
	MethodItemCompleted         = "item/completed"
)

var notificationMethodSet = map[string]struct{}{
	MethodTurnStarted:           {},
	MethodTurnCompleted:         {},
	MethodItemStarted:           {},
	MethodItemAgentMessageDelta: {},
	MethodItemCompleted:         {},
}

var serverRequestMethodSet = map[string]struct{}{
	MethodItemPermissionsRequestApproval:      {},
	MethodItemFileChangeRequestApproval:       {},
	MethodItemCommandExecutionRequestApproval: {},
}

// IsNotificationMethod reports whether the given method name is a server → client notification.
func IsNotificationMethod(method string) bool {
	_, ok := notificationMethodSet[method]
	return ok
}

// IsServerRequestMethod reports whether the given method is a server → client request requiring a response.
func IsServerRequestMethod(method string) bool {
	_, ok := serverRequestMethodSet[method]
	return ok
}
