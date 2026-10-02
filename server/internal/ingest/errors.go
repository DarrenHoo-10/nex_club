package ingest

import "time"

// RetryableError asks River to snooze the fetch. The checkpoint is left unchanged.
type RetryableError struct {
	After time.Duration
	Err   error
}

func (e *RetryableError) Error() string {
	if e == nil {
		return "来源暂时不可用"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "来源暂时不可用"
}

func (e *RetryableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// PermanentError fails the run and must not be retried.
type PermanentError struct {
	Code string
	Err  error
}

func (e *PermanentError) Error() string {
	if e == nil {
		return "采集失败"
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Code != "" {
		return e.Code
	}
	return "采集失败"
}

func (e *PermanentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
