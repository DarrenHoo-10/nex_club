-- +goose Up
REVOKE ALL ON TABLE audit_logs FROM PUBLIC;
GRANT SELECT, INSERT ON TABLE audit_logs TO nex_app;
GRANT USAGE, SELECT ON SEQUENCE audit_logs_id_seq TO nex_app;

-- +goose Down
REVOKE ALL ON SEQUENCE audit_logs_id_seq FROM nex_app;
REVOKE ALL ON TABLE audit_logs FROM nex_app;
