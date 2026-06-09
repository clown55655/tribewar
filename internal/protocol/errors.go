package protocol

import "fmt"

const CurrentVersion uint16 = 1

type ErrorCode int32

const (
	CodeOK ErrorCode = 0

	CodeInvalidRequest ErrorCode = 1001
	CodeUnauthorized   ErrorCode = 1002
	CodeForbidden      ErrorCode = 1003
	CodeNotFound       ErrorCode = 1004
	CodeTimeout        ErrorCode = 1005
	CodeUnavailable    ErrorCode = 1006
	CodeCircuitOpen    ErrorCode = 1007
	CodeInternal       ErrorCode = 1008

	CodeBusinessFailed ErrorCode = 2001
	CodeConflict       ErrorCode = 2002
	CodeIdempotentHit  ErrorCode = 2003
)

type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%d %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%d %s", e.Code, e.Message)
}

func Wrap(code ErrorCode, message string, cause error) error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func NewError(code ErrorCode, message string) error {
	return &Error{Code: code, Message: message}
}
