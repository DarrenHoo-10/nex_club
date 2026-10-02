-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nex_app') THEN
        CREATE ROLE nex_app NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Leave nex_app in place. Dropping the role fails while grants still reference it,
-- and a later up must find the same role.
SELECT 1;
