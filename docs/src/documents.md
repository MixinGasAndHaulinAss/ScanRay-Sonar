# Site documents

**Documents** (sidebar) stores operator-uploaded files for a site—runbooks, diagrams, vendor PDFs. Files are stored encrypted at rest when object storage is configured.

This is **not** the product manual. For how Sonar works, use **Documentation** (this MkDocs guide).

### Object storage (MinIO)

Compose includes `sonar-minio`. Set `SONAR_MINIO_ENDPOINT` (typically `sonar-minio:9000`), `SONAR_MINIO_USER`, `SONAR_MINIO_PASSWORD`, and optionally `SONAR_MINIO_BUCKET` / `SONAR_MINIO_SSL` on the API so uploads go to MinIO. If those vars are unset, Sonar falls back to storing document bodies in Postgres. See [Installation](installation.md#3-minio-site-documents).

## Upload and download

Requires **siteadmin** to upload.

1. Open **Documents**.
2. Choose the site and upload a file.
3. Anyone who can see the site can download (subject to your deployment’s auth).
4. Version history is available per document when the UI exposes it.

Treat documents as sensitive operational content; do not upload secrets that belong in the credential vault (use site credentials instead).
