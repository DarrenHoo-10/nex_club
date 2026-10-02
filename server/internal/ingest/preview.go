package ingest

import (
	"context"
	"errors"
)

// Preview calls only an adapter. It never writes raw items, checkpoints, or processing jobs.
// Paid adapters may write provider receipts through their separate billing port.
func (s *Service) Preview(ctx context.Context, src Source) (FetchBatch, error) {
	adapter := s.adapter(src.Kind)
	if adapter == nil {
		return FetchBatch{}, &PermanentError{Code: "unsupported_source", Err: errors.New("此类型不支持主动试抓")}
	}
	src.Checkpoint = nil
	return adapter.Fetch(ctx, src)
}
