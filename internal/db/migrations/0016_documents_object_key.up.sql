-- =============================================================================
-- 0016_documents_object_key.up.sql — scaffold MinIO streaming.
-- =============================================================================
--
-- documents/document_versions still keep their encrypted Postgres BYTEA copies
-- so existing reads keep working; new uploads will additionally populate
-- object_key when MinIO streaming lands. The handler picks the source at read
-- time (object_key first, fall back to enc_body).

ALTER TABLE documents
  ADD COLUMN IF NOT EXISTS object_key TEXT,
  ADD COLUMN IF NOT EXISTS bucket TEXT;

ALTER TABLE document_versions
  ADD COLUMN IF NOT EXISTS object_key TEXT,
  ADD COLUMN IF NOT EXISTS bucket TEXT;

CREATE INDEX IF NOT EXISTS documents_object_key_idx ON documents(object_key);
-- document_versions already has document_versions_doc_idx (document_id, created_at DESC)
-- from migration 0013, no extra index needed for the listing endpoint.
