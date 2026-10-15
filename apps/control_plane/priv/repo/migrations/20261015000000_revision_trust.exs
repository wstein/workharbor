# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Repo.Migrations.RevisionTrust do
  use Ecto.Migration

  def up do
    execute("""
    CREATE TABLE revision_trust_approvals (
      id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL REFERENCES owners(id),
      descriptor TEXT NOT NULL, descriptor_bytes BLOB NOT NULL, descriptor_sha256 TEXT NOT NULL,
      approved_at INTEGER NOT NULL CHECK(typeof(approved_at)='integer' AND approved_at>=0),
      inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL,
      UNIQUE(owner_id,descriptor_sha256), UNIQUE(owner_id,descriptor_bytes))
    """)

    execute(
      "CREATE TRIGGER revision_approval_immutable BEFORE UPDATE ON revision_trust_approvals WHEN 1 BEGIN SELECT RAISE(ABORT, 'immutable revision approval'); END"
    )

    execute(
      "CREATE TRIGGER revision_approval_retained BEFORE DELETE ON revision_trust_approvals WHEN 1 BEGIN SELECT RAISE(ABORT, 'retained revision approval'); END"
    )
  end

  def down do
    execute("DROP TRIGGER revision_approval_retained")
    execute("DROP TRIGGER revision_approval_immutable")
    execute("DROP TABLE revision_trust_approvals")
  end
end
