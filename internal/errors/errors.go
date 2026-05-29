package errors

import (
	stderrors "errors"
	"fmt"
)

// 业务错误类型
type BotError struct {
	Code    string
	Message string
	Err     error
}

func (e *BotError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *BotError) Unwrap() error {
	return e.Err
}

func (e *BotError) HasCode(code string) bool {
	return e != nil && e.Code == code
}

// 预定义错误代码
const (
	ErrCodeAPICall           = "API_CALL"
	ErrCodeRateLimit         = "RATE_LIMIT"
	ErrCodeAPITimeout        = "API_TIMEOUT"
	ErrCodeAPIHTTPStatus     = "API_HTTP_STATUS"
	ErrCodeAPIDecode         = "API_DECODE"
	ErrCodeConfig            = "CONFIG"
	ErrCodeInvalidInput      = "INVALID_INPUT"
	ErrCodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	ErrCodeOrderFailed       = "ORDER_FAILED"
	ErrCodeAuthentication    = "AUTH_FAILED"
)

// 创建错误的便利函数
func NewAPIError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeAPICall, Message: message, Err: err}
}

func NewRateLimitError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeRateLimit, Message: message, Err: err}
}

func NewAPITimeoutError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeAPITimeout, Message: message, Err: err}
}

func NewAPIHTTPStatusError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeAPIHTTPStatus, Message: message, Err: err}
}

func NewAPIDecodeError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeAPIDecode, Message: message, Err: err}
}

func NewAuthenticationError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeAuthentication, Message: message, Err: err}
}

func NewConfigError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeConfig, Message: message, Err: err}
}

func NewValidationError(message string) *BotError {
	return &BotError{Code: ErrCodeInvalidInput, Message: message}
}

func NewOrderError(message string, err error) *BotError {
	return &BotError{Code: ErrCodeOrderFailed, Message: message, Err: err}
}

func HasCode(err error, code string) bool {
	var botErr *BotError
	return stderrors.As(err, &botErr) && botErr.HasCode(code)
}
