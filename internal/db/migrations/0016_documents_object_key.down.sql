DROP INDEX IF EXISTS documents_object_key_idx;
ALTER TABLE documents DROP COLUMN IF EXISTS object_key, DROP COLUMN IF EXISTS bucket;
ALTER TABLE document_versions DROP COLUMN IF EXISTS object_key, DROP COLUMN IF EXISTS bucket;
