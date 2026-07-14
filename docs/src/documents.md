# Site documents

**Documents** (sidebar) stores operator-uploaded files for a site—runbooks, diagrams, vendor PDFs. Files are stored encrypted at rest when object storage is configured.

This is **not** the product manual. For how Sonar works, use **Documentation** (this MkDocs guide).

## Upload and download

Requires **siteadmin** to upload.

1. Open **Documents**.
2. Choose the site and upload a file.
3. Anyone who can see the site can download (subject to your deployment’s auth).
4. Version history is available per document when the UI exposes it.

Treat documents as sensitive operational content; do not upload secrets that belong in the credential vault (use site credentials instead).
