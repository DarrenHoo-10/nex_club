-- Paid collector previews are receipts, not content items or processing runs.
ALTER TABLE provider_calls ADD COLUMN preview_request_key text;
ALTER TABLE provider_calls ADD COLUMN request_summary jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(request_summary)='object');
ALTER TABLE provider_calls DROP CONSTRAINT provider_calls_check;
ALTER TABLE provider_calls ADD CONSTRAINT provider_calls_context_check
    CHECK (num_nonnulls(processing_run_id, source_run_id, preview_request_key) = 1);
ALTER TABLE provider_calls ADD CONSTRAINT provider_calls_preview_key_check
    CHECK (preview_request_key IS NULL OR (length(preview_request_key) BETWEEN 1 AND 200));
CREATE INDEX provider_calls_preview_idx ON provider_calls (preview_request_key) WHERE preview_request_key IS NOT NULL;
