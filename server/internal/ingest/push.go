package ingest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/ingest/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	platformhttp "github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/sources/push"
)

const (
	maxPushBody  = 1 << 20
	maxPushItems = 50
	batchTTL     = 24 * time.Hour
	batchScope   = "ingest.batch.v1"
)

type batchMeta struct {
	ItemHashes        []string `json:"item_hashes"`
	ItemCount         int      `json:"item_count"`
	SourceID          string   `json:"source_id"`
	SourceEditVersion int64    `json:"source_edit_version"`
}

type storedBatch struct {
	ID             uuid.UUID
	PrincipalKey   string
	Scope          string
	RequestHash    string
	Status         string
	ResponseStatus *int16
	ResponseBody   []byte
	RequestMeta    json.RawMessage
}

type publicItem struct {
	SourceItemKey string `json:"source_item_key"`
	Status        string `json:"status"`
	RawItemID     string `json:"raw_item_id,omitempty"`
	Code          string `json:"code,omitempty"`
}

// Mount registers POST /api/ingest/v1/items.
func Mount(mux *http.ServeMux, s *Service) {
	if mux == nil || s == nil {
		return
	}
	mux.HandleFunc("POST /api/ingest/v1/items", s.handlePush)
}

func (s *Service) handlePush(w http.ResponseWriter, r *http.Request) {
	body, err := readPushBody(r)
	if err != nil {
		writePushErr(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if err := adminauth.ValidateIdempotencyKey(key); err != nil {
		writePushErr(w, r, err)
		return
	}
	requestHash, err := adminauth.HashJSON(body)
	if err != nil {
		writePushErr(w, r, apperr.Invalid("请求体不是合法的 JSON"))
		return
	}
	batch, err := push.Parse(body)
	if err != nil {
		writePushErr(w, r, err)
		return
	}
	if len(batch.Items) == 0 {
		writePushErr(w, r, apperr.Invalid("条目不能为空"))
		return
	}
	if len(batch.Items) > maxPushItems {
		writePushErr(w, r, apperr.Invalid("单次最多 50 条"))
		return
	}
	cred, err := s.authenticate(r.Context(), r)
	if err != nil {
		writePushErr(w, r, err)
		return
	}
	if cred.SourceID != batch.SourceID {
		writePushErr(w, r, apperr.Forbidden("不能向其他信源推送"))
		return
	}
	hashes := make([]string, len(batch.Items))
	for i, item := range batch.Items {
		sum, err := adminauth.HashJSON(item.Raw)
		if err != nil {
			writePushErr(w, r, apperr.Invalid("请求体不是合法的 JSON"))
			return
		}
		hashes[i] = sum
	}
	meta := batchMeta{
		ItemHashes:        hashes,
		ItemCount:         len(batch.Items),
		SourceID:          cred.SourceID.String(),
		SourceEditVersion: cred.EditVersion,
	}
	principal := "ingest:" + cred.ID.String()
	stored, replay, err := s.registerParent(r.Context(), principal, key, requestHash, meta, cred.ID)
	if err != nil {
		writePushErr(w, r, err)
		return
	}
	if replay {
		writeStored(w, stored.ResponseStatus, stored.ResponseBody)
		return
	}
	for i, item := range batch.Items {
		raw, err := s.processItem(r.Context(), stored, principal, requestHash, meta, i, item, hashes[i])
		if err != nil {
			writePushErr(w, r, err)
			return
		}
		if s.onItemCommitted != nil {
			if err := s.onItemCommitted(i); err != nil {
				writePushErr(w, r, apperr.Unavailable("采集暂不可用，请稍后重试"))
				return
			}
		}
		_ = raw
	}
	if s.onBeforeFinalize != nil {
		if err := s.onBeforeFinalize(); err != nil {
			writePushErr(w, r, apperr.Unavailable("采集暂不可用，请稍后重试"))
			return
		}
	}
	status, raw, err := s.finalize(r.Context(), stored.ID, principal, meta.ItemCount)
	if err != nil {
		writePushErr(w, r, err)
		return
	}
	writeStored(w, statusCode(status), raw)
}

func readPushBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, apperr.Invalid("请求体不是合法的 JSON")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPushBody+1))
	if err != nil {
		return nil, apperr.Invalid("请求体不是合法的 JSON")
	}
	if len(body) > maxPushBody {
		return nil, apperr.Invalid("请求体不能超过 1MB")
	}
	if len(body) == 0 {
		return nil, apperr.Invalid("请求体不是合法的 JSON")
	}
	return body, nil
}

type credential struct {
	ID          uuid.UUID
	SourceID    uuid.UUID
	EditVersion int64
}

func (s *Service) authenticate(ctx context.Context, r *http.Request) (credential, error) {
	token, ok := push.BearerToken(r.Header.Get("Authorization"))
	if !ok {
		return credential{}, apperr.Unauthenticated("缺少访问令牌")
	}
	row, err := sqlc.New(s.pool).FindCredentialByHash(ctx, push.TokenHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return credential{}, apperr.Unauthenticated("令牌无效")
	}
	if err != nil {
		return credential{}, err
	}
	now := s.now()
	if row.RevokedAt.Valid {
		return credential{}, apperr.Unauthenticated("令牌已撤销")
	}
	if row.ExpiresAt.Valid && !row.ExpiresAt.Time.After(now) {
		return credential{}, apperr.Unauthenticated("令牌已过期")
	}
	if !hasScope(row.Scopes, "items:write") {
		return credential{}, apperr.Forbidden("令牌缺少写入权限")
	}
	if !row.Enabled || row.TrustTier == "excluded" {
		return credential{}, apperr.New("source_disabled", "信源已暂停", http.StatusForbidden)
	}
	return credential{ID: row.ID, SourceID: row.SourceID, EditVersion: row.EditVersion}, nil
}

func hasScope(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

func (s *Service) registerParent(ctx context.Context, principal, key, requestHash string, meta batchMeta, credID uuid.UUID) (storedBatch, bool, error) {
	var stored storedBatch
	var replay bool
	err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
			return err
		}
		q := sqlc.New(tx)
		existing, err := q.GetIngestBatch(ctx, sqlc.GetIngestBatchParams{PrincipalKey: principal, IdempotencyKey: key})
		if err == nil {
			return s.takeExistingBatch(existing, requestHash, &stored, &replay)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		rawMeta, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		inserted, err := q.InsertIngestBatch(ctx, sqlc.InsertIngestBatchParams{
			ID:             uuid.New(),
			PrincipalKey:   principal,
			IdempotencyKey: key,
			RequestHash:    requestHash,
			ExpiresAt:      s.now().Add(batchTTL),
			RequestMeta:    rawMeta,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			existing, err = q.GetIngestBatch(ctx, sqlc.GetIngestBatchParams{PrincipalKey: principal, IdempotencyKey: key})
			if err != nil {
				return err
			}
			return s.takeExistingBatch(existing, requestHash, &stored, &replay)
		}
		if err != nil {
			return err
		}
		if err := q.TouchCredentialUsed(ctx, sqlc.TouchCredentialUsedParams{LastUsedAt: pgTime(s.now()), ID: credID}); err != nil {
			return err
		}
		stored = storedBatch{
			ID:           inserted.ID,
			PrincipalKey: inserted.PrincipalKey,
			Scope:        inserted.Scope,
			RequestHash:  inserted.RequestHash,
			Status:       inserted.Status,
			RequestMeta:  inserted.RequestMeta,
		}
		return nil
	})
	return stored, replay, err
}

func (s *Service) takeExistingBatch(row sqlc.GetIngestBatchRow, requestHash string, stored *storedBatch, replay *bool) error {
	if !sameHash(row.RequestHash, requestHash) {
		return apperr.IdempotencyMismatch("幂等键已被用于不同的请求")
	}
	*stored = storedBatch{
		ID:             row.ID,
		PrincipalKey:   row.PrincipalKey,
		Scope:          row.Scope,
		RequestHash:    row.RequestHash,
		Status:         row.Status,
		ResponseStatus: row.ResponseStatus,
		ResponseBody:   append([]byte(nil), row.ResponseBody...),
		RequestMeta:    append(json.RawMessage(nil), row.RequestMeta...),
	}
	if row.Status == "completed" {
		*replay = true
		return nil
	}
	if row.Status != "processing" {
		return apperr.Internal("批次状态异常")
	}
	return nil
}

func (s *Service) processItem(ctx context.Context, parent storedBatch, principal, requestHash string, meta batchMeta, index int, item push.Item, itemHash string) (json.RawMessage, error) {
	var body json.RawMessage
	err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
			return err
		}
		q := sqlc.New(tx)
		locked, err := q.LockIngestParentShare(ctx, sqlc.LockIngestParentShareParams{ID: parent.ID, PrincipalKey: principal})
		if err != nil {
			return err
		}
		if locked.Scope != batchScope || !sameHash(locked.RequestHash, requestHash) {
			return apperr.IdempotencyMismatch("幂等键已被用于不同的请求")
		}
		var storedMeta batchMeta
		if err := json.Unmarshal(locked.RequestMeta, &storedMeta); err != nil {
			return err
		}
		if index < 0 || index >= len(storedMeta.ItemHashes) || !sameHash(storedMeta.ItemHashes[index], itemHash) {
			return apperr.IdempotencyMismatch("幂等键已被用于不同的请求")
		}
		scope := itemScope(parent.ID)
		key := strconv.Itoa(index)
		if locked.Status == "completed" {
			raw, err := completedChild(ctx, q, principal, scope, key, itemHash)
			if err != nil {
				return err
			}
			body = raw
			return nil
		}
		if locked.Status != "processing" {
			return apperr.Internal("批次状态异常")
		}
		childID, done, raw, err := s.claimChild(ctx, q, principal, scope, key, itemHash, parent.ID, index)
		if err != nil {
			return err
		}
		if done {
			body = raw
			return nil
		}
		var res ItemResult
		if _, err := tx.Exec(ctx, "SAVEPOINT ingest_item"); err != nil {
			return err
		}
		if item.Reject {
			res = ItemResult{SourceItemKey: item.SourceItemKey, Status: StatusRejected, Code: "invalid_argument"}
		} else {
			res, err = s.AcceptTx(ctx, tx, parentSource(meta), meta.SourceEditVersion, incomingFrom(item))
			if err != nil {
				return err
			}
		}
		if res.Status == StatusRejected || res.Status == StatusStale {
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT ingest_item"); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT ingest_item"); err != nil {
			return err
		}
		raw, err = s.completeChild(ctx, q, childID, principal, res)
		if err != nil {
			return err
		}
		body = raw
		return nil
	})
	return body, err
}

func (s *Service) claimChild(ctx context.Context, q *sqlc.Queries, principal, scope, key, itemHash string, parentID uuid.UUID, index int) (uuid.UUID, bool, []byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		child, err := q.InsertIngestChild(ctx, sqlc.InsertIngestChildParams{
			ID:             uuid.New(),
			PrincipalKey:   principal,
			Scope:          scope,
			IdempotencyKey: key,
			RequestHash:    itemHash,
			ExpiresAt:      s.now().Add(batchTTL),
			ParentID:       pgUUID(parentID),
			ItemIndex:      indexPtr(index),
		})
		if err == nil {
			return child.ID, false, nil, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil, err
		}
		row, err := q.LockIngestChild(ctx, sqlc.LockIngestChildParams{PrincipalKey: principal, Scope: scope, IdempotencyKey: key})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return uuid.Nil, false, nil, err
		}
		if !sameHash(row.RequestHash, itemHash) {
			return uuid.Nil, false, nil, apperr.IdempotencyMismatch("幂等键已被用于不同的请求")
		}
		if row.Status == "completed" {
			return row.ID, true, append([]byte(nil), row.ResponseBody...), nil
		}
		return row.ID, false, nil, nil
	}
	return uuid.Nil, false, nil, apperr.Unavailable("采集暂不可用，请稍后重试")
}

func completedChild(ctx context.Context, q *sqlc.Queries, principal, scope, key, itemHash string) ([]byte, error) {
	row, err := q.LockIngestChild(ctx, sqlc.LockIngestChildParams{PrincipalKey: principal, Scope: scope, IdempotencyKey: key})
	if err != nil {
		return nil, err
	}
	if row.Status != "completed" || !sameHash(row.RequestHash, itemHash) {
		return nil, apperr.Unavailable("采集暂不可用，请稍后重试")
	}
	return append([]byte(nil), row.ResponseBody...), nil
}

func (s *Service) completeChild(ctx context.Context, q *sqlc.Queries, id uuid.UUID, principal string, res ItemResult) ([]byte, error) {
	raw, err := json.Marshal(toPublic(res))
	if err != nil {
		return nil, err
	}
	n, err := q.CompleteIngestRequest(ctx, sqlc.CompleteIngestRequestParams{
		ResponseStatus: statusCode(http.StatusOK),
		ResponseBody:   raw,
		ExpiresAt:      s.now().Add(batchTTL),
		ID:             id,
		PrincipalKey:   principal,
	})
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, apperr.Internal("条目结果未能保存")
	}
	return raw, nil
}

func (s *Service) finalize(ctx context.Context, parentID uuid.UUID, principal string, count int) (int, []byte, error) {
	var status int
	var body []byte
	err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
			return err
		}
		q := sqlc.New(tx)
		parent, err := q.LockIngestParentUpdate(ctx, sqlc.LockIngestParentUpdateParams{ID: parentID, PrincipalKey: principal})
		if err != nil {
			return err
		}
		if parent.Status == "completed" {
			if parent.ResponseStatus == nil || len(parent.ResponseBody) == 0 {
				return apperr.Internal("批次响应缺失")
			}
			status = int(*parent.ResponseStatus)
			body = append([]byte(nil), parent.ResponseBody...)
			return nil
		}
		kids, err := q.ListIngestChildren(ctx, sqlc.ListIngestChildrenParams{ParentID: pgUUID(parentID), PrincipalKey: principal})
		if err != nil {
			return err
		}
		if len(kids) != count {
			return apperr.Unavailable("采集暂不可用，请稍后重试")
		}
		results := make([]json.RawMessage, count)
		for _, kid := range kids {
			if kid.ItemIndex == nil || kid.Status != "completed" || len(kid.ResponseBody) == 0 {
				return apperr.Unavailable("采集暂不可用，请稍后重试")
			}
			i := int(*kid.ItemIndex)
			if i < 0 || i >= count || results[i] != nil {
				return apperr.Unavailable("采集暂不可用，请稍后重试")
			}
			results[i] = append(json.RawMessage(nil), kid.ResponseBody...)
		}
		for _, raw := range results {
			if len(raw) == 0 {
				return apperr.Unavailable("采集暂不可用，请稍后重试")
			}
		}
		raw, err := json.Marshal(struct {
			Results []json.RawMessage `json:"results"`
		}{Results: results})
		if err != nil {
			return err
		}
		n, err := q.CompleteIngestRequest(ctx, sqlc.CompleteIngestRequestParams{
			ResponseStatus: statusCode(http.StatusOK),
			ResponseBody:   raw,
			ExpiresAt:      s.now().Add(batchTTL),
			ID:             parentID,
			PrincipalKey:   principal,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return apperr.Internal("批次结果未能保存")
		}
		status = http.StatusOK
		body = raw
		return nil
	})
	return status, body, err
}

func toPublic(res ItemResult) publicItem {
	status := res.Status
	code := res.Code
	if status == StatusStale {
		status = StatusRejected
		code = "stale_source"
	}
	out := publicItem{SourceItemKey: res.SourceItemKey, Status: status, Code: code}
	if res.RawItemID != nil && status != StatusRejected {
		out.RawItemID = res.RawItemID.String()
	}
	return out
}

func incomingFrom(item push.Item) IncomingItem {
	return IncomingItem{
		SourceItemKey: item.SourceItemKey,
		URL:           item.URL,
		Title:         item.Title,
		Excerpt:       item.Excerpt,
		BodyText:      item.BodyText,
		BodyHTML:      item.BodyHTML,
		PublishedAt:   item.PublishedAt,
		GUID:          item.GUID,
		Permalink:     item.Permalink,
		GitHubID:      item.GitHubID,
		Payload:       item.Payload,
	}
}

func parentSource(meta batchMeta) uuid.UUID {
	id, err := uuid.Parse(meta.SourceID)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func itemScope(parent uuid.UUID) string {
	return "ingest.item.v1:" + parent.String()
}

func sameHash(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeStored(w http.ResponseWriter, status *int16, body []byte) {
	code := http.StatusOK
	if status != nil {
		code = int(*status)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func writePushErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		platformhttp.WriteError(w, r, ae)
		return
	}
	if isLockTimeout(err) {
		platformhttp.WriteError(w, r, apperr.RequestBusy())
		return
	}
	slog.Error("ingest push", "err", err)
	platformhttp.WriteError(w, r, apperr.Unavailable("采集暂不可用，请稍后重试"))
}

func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}
