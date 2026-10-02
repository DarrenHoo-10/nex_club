-- +goose Up
-- Paid collector previews are receipts, not content items or processing runs.
ALTER TABLE provider_calls ADD COLUMN preview_request_key text;
ALTER TABLE provider_calls ADD COLUMN request_summary jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(request_summary)='object');
ALTER TABLE provider_calls DROP CONSTRAINT provider_calls_check;
ALTER TABLE provider_calls ADD CONSTRAINT provider_calls_context_check
    CHECK (num_nonnulls(processing_run_id, source_run_id, preview_request_key) = 1);
ALTER TABLE provider_calls ADD CONSTRAINT provider_calls_preview_key_check
    CHECK (preview_request_key IS NULL OR (length(preview_request_key) BETWEEN 1 AND 200));
CREATE INDEX provider_calls_preview_idx ON provider_calls (preview_request_key) WHERE preview_request_key IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM provider_calls WHERE preview_request_key IS NOT NULL) THEN
  RAISE EXCEPTION 'Paid preview receipts exist; preserve and reconcile them before downgrading';
 END IF;
END $$;
-- +goose StatementEnd
DROP INDEX provider_calls_preview_idx;
ALTER TABLE provider_calls DROP CONSTRAINT provider_calls_preview_key_check;
ALTER TABLE provider_calls DROP CONSTRAINT provider_calls_context_check;
ALTER TABLE provider_calls DROP COLUMN preview_request_key;
ALTER TABLE provider_calls DROP COLUMN request_summary;
ALTER TABLE provider_calls ADD CONSTRAINT provider_calls_check CHECK ((processing_run_id IS NULL) <> (source_run_id IS NULL));
