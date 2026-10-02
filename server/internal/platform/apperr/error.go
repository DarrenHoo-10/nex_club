package apperr

import "net/http"

type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

type Error struct {
	Code       string       `json:"code"`
	Message    string       `json:"message"`
	HTTPStatus int          `json:"-"`
	Fields     []FieldError `json:"-"`
	Op         string       `json:"-"`
	Err        error        `json:"-"`
}

func (e *Error) Error() string {
	if e.Op != "" {
		return e.Op + ": " + e.Message
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func New(code, message string, status int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: status, Fields: []FieldError{}}
}

func Invalid(message string, fields ...FieldError) *Error {
	e := New("invalid_argument", message, http.StatusBadRequest)
	e.Fields = fields
	return e
}

func Unauthenticated(message string) *Error {
	return New("unauthenticated", message, http.StatusUnauthorized)
}

func Forbidden(message string) *Error {
	return New("forbidden", message, http.StatusForbidden)
}

func NotFound(message string) *Error {
	return New("not_found", message, http.StatusNotFound)
}

func EditConflict(message string) *Error {
	return New("edit_conflict", message, http.StatusConflict)
}

func DraftConflict(message string) *Error {
	return New("draft_conflict", message, http.StatusConflict)
}

func IdempotencyMismatch(message string) *Error {
	return New("idempotency_mismatch", message, http.StatusConflict)
}

func RequestBusy() *Error {
	return New("request_busy", "请求正在处理，请用相同内容稍后重试", http.StatusServiceUnavailable)
}

func CursorStale() *Error {
	return New("cursor_stale", "列表已变化，请从第一页重新加载", http.StatusBadRequest)
}

func RateLimited(message string) *Error {
	return New("rate_limited", message, http.StatusTooManyRequests)
}

func NotImplemented() *Error {
	return New("not_implemented", "接口尚未实现", http.StatusNotImplemented)
}

func Unavailable(message string) *Error {
	return New("unavailable", message, http.StatusServiceUnavailable)
}

func Internal(message string) *Error {
	return New("internal", message, http.StatusInternalServerError)
}

func ModelDisabled() *Error {
	return New("model_disabled", "模型调用已关闭", http.StatusServiceUnavailable)
}
