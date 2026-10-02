ALTER TABLE assistant_actions RENAME TO mcp_actions;
ALTER TABLE mcp_tokens ADD COLUMN can_execute boolean NOT NULL DEFAULT false;
ALTER TABLE mcp_tokens ADD CONSTRAINT mcp_token_execution_requires_prepare CHECK(NOT can_execute OR can_prepare);
-- Retain the short-lived embedded-chat trial's rows solely for immutable billing history.
-- There are no active chat HTTP endpoints or chat model calls after this migration.
