package line

type ErrorCode string

const (
	ErrUnsupportedPlatform ErrorCode = "UNSUPPORTED_PLATFORM"
	ErrLineNotRunning      ErrorCode = "LINE_NOT_RUNNING"
	ErrMemoryReadFailed    ErrorCode = "MEMORY_READ_FAILED"
	ErrKeyScanEmpty        ErrorCode = "KEY_SCAN_EMPTY"
	ErrNoKeyDecrypted      ErrorCode = "NO_KEY_DECRYPTED"
	ErrDBNotFound          ErrorCode = "DB_NOT_FOUND"
	ErrDBAccess            ErrorCode = "DB_ACCESS"
	ErrDBLocked            ErrorCode = "DB_LOCKED"
	ErrDBUnreadable        ErrorCode = "DB_UNREADABLE"
	ErrChatNotFound        ErrorCode = "CHAT_NOT_FOUND"
	ErrChatAmbiguous       ErrorCode = "CHAT_AMBIGUOUS"
	ErrSenderNotFound      ErrorCode = "SENDER_NOT_FOUND"
	ErrInvalidArgument     ErrorCode = "INVALID_ARGUMENT"
	ErrTokenNotFound       ErrorCode = "TOKEN_NOT_FOUND"
	ErrE2EEKeyMissing      ErrorCode = "E2EE_KEY_MISSING"
	ErrSendFailed          ErrorCode = "SEND_FAILED"
	ErrSendUncertain       ErrorCode = "SEND_UNCERTAIN"
	ErrE2EEDisabled        ErrorCode = "E2EE_DISABLED"
	ErrDuplicateSend       ErrorCode = "DUPLICATE_SEND"
	ErrSenderAmbiguous     ErrorCode = "SENDER_AMBIGUOUS"
	ErrUpdateCheckFailed   ErrorCode = "UPDATE_CHECK_FAILED"
)

type Error struct {
	Code       ErrorCode
	Message    string
	Candidates []ChatSummary     // CHAT_AMBIGUOUS
	Senders    []SenderCandidate // SENDER_AMBIGUOUS
	schema     bool              // DB_UNREADABLE because the schema changed
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// SchemaChanged reports a DB_UNREADABLE error caused by a changed database
// schema, which reading again cannot fix.
func (e *Error) SchemaChanged() bool { return e.schema }

// CandidateList is what the error lists under "candidates": chats or people,
// or nil when there are none.
func (e *Error) CandidateList() any {
	switch {
	case len(e.Candidates) > 0:
		return e.Candidates
	case len(e.Senders) > 0:
		return e.Senders
	}
	return nil
}

func AsError(err error) (*Error, bool) {
	le, ok := err.(*Error)
	return le, ok
}
