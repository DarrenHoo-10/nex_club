CREATE TABLE assistant_conversations (
 id uuid PRIMARY KEY, admin_id uuid NOT NULL REFERENCES admin_users(id),
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 120), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assistant_conversations_owner ON assistant_conversations(admin_id,created_at DESC);
CREATE TABLE assistant_turns (
 id uuid PRIMARY KEY, conversation_id uuid NOT NULL REFERENCES assistant_conversations(id),
 request_key text NOT NULL, user_text text NOT NULL CHECK(length(user_text) BETWEEN 1 AND 8000),
 profile_version text NOT NULL, state jsonb NOT NULL DEFAULT '{}'::jsonb,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed')),
 reply text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conversation_id,request_key)
);
CREATE INDEX assistant_turns_conversation ON assistant_turns(conversation_id,created_at,id);
CREATE TABLE assistant_actions (
 id uuid PRIMARY KEY, admin_id uuid NOT NULL REFERENCES admin_users(id),
 conversation_id uuid REFERENCES assistant_conversations(id), request_key text NOT NULL,
 operation text NOT NULL, arguments jsonb NOT NULL CHECK(jsonb_typeof(arguments)='object'),
 preview jsonb NOT NULL, status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','completed','rejected')),
 result jsonb, created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT (now()+interval '24 hours'),
 UNIQUE(admin_id,request_key)
);
CREATE INDEX assistant_actions_owner ON assistant_actions(admin_id,created_at DESC);
CREATE TABLE mcp_tokens (
 id uuid PRIMARY KEY, admin_id uuid NOT NULL REFERENCES admin_users(id), name text NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
 token_hash text NOT NULL UNIQUE, can_prepare boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL, revoked_at timestamptz
);
ALTER TABLE provider_calls ADD COLUMN assistant_turn_id uuid REFERENCES assistant_turns(id);
ALTER TABLE provider_calls DROP CONSTRAINT provider_calls_context_check;
ALTER TABLE provider_calls ADD CONSTRAINT provider_calls_context_check
 CHECK(num_nonnulls(processing_run_id,source_run_id,preview_request_key,assistant_turn_id)=1);
CREATE INDEX provider_calls_assistant_idx ON provider_calls(assistant_turn_id) WHERE assistant_turn_id IS NOT NULL;
