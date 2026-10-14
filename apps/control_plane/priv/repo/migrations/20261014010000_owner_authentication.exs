# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Repo.Migrations.OwnerAuthentication do
  use Ecto.Migration

  def up do
    execute(
      "CREATE TABLE owner_authenticators (id INTEGER PRIMARY KEY CHECK(id=1), owner_id TEXT NOT NULL UNIQUE REFERENCES owners(id), secret_hash BLOB NOT NULL CHECK(length(secret_hash)=32))"
    )

    execute(
      "CREATE TABLE owner_credentials (id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL REFERENCES owners(id), token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash)=32), kind TEXT NOT NULL CHECK(kind IN ('cli_owner','owner_browser','dashboard')), issued_at INTEGER NOT NULL CHECK(typeof(issued_at)='integer'), expires_at INTEGER NOT NULL CHECK(typeof(expires_at)='integer' AND expires_at>issued_at), revoked_at INTEGER CHECK(revoked_at IS NULL OR typeof(revoked_at)='integer'), CHECK(expires_at-issued_at<=CASE WHEN kind='cli_owner' THEN 2592000 ELSE 28800 END))"
    )

    execute(
      "CREATE TRIGGER owner_authenticator_immutable BEFORE UPDATE ON owner_authenticators BEGIN SELECT RAISE(ABORT,'authentication invariant'); END"
    )

    execute(
      "CREATE TRIGGER owner_credential_identity_immutable BEFORE UPDATE ON owner_credentials WHEN NEW.id IS NOT OLD.id OR NEW.owner_id IS NOT OLD.owner_id OR NEW.token_hash IS NOT OLD.token_hash OR NEW.kind IS NOT OLD.kind OR NEW.issued_at IS NOT OLD.issued_at OR NEW.expires_at IS NOT OLD.expires_at OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS NOT OLD.revoked_at) BEGIN SELECT RAISE(ABORT,'authentication invariant'); END"
    )

    execute(
      "CREATE TRIGGER owner_authenticator_retained BEFORE DELETE ON owner_authenticators BEGIN SELECT RAISE(ABORT,'authentication invariant'); END"
    )

    execute(
      "CREATE TRIGGER owner_credentials_retained BEFORE DELETE ON owner_credentials BEGIN SELECT RAISE(ABORT,'authentication invariant'); END"
    )
  end

  def down do
    drop table(:owner_credentials)
    drop table(:owner_authenticators)
  end
end
