# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Repo.Migrations.HostIdentity do
  use Ecto.Migration

  def up do
    execute(
      "CREATE TABLE host_security (host_id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL, revoked_at INTEGER, FOREIGN KEY(host_id,owner_id) REFERENCES hosts(id,owner_id))"
    )

    execute(
      "CREATE TABLE host_enrollments (id TEXT PRIMARY KEY NOT NULL, host_id TEXT NOT NULL, owner_id TEXT NOT NULL, code_hash BLOB UNIQUE NOT NULL, issued_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, consumed_at INTEGER, revoked_at INTEGER, FOREIGN KEY(host_id,owner_id) REFERENCES hosts(id,owner_id), CHECK(expires_at=issued_at+300))"
    )

    execute(
      "CREATE TABLE host_identities (id TEXT PRIMARY KEY NOT NULL, host_id TEXT NOT NULL, owner_id TEXT NOT NULL, fingerprint TEXT UNIQUE NOT NULL, certificate BLOB NOT NULL, csr_hash BLOB NOT NULL, predecessor_id TEXT, status TEXT NOT NULL CHECK(status IN ('active','pending','retired','revoked')), issued_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, activated_at INTEGER, UNIQUE(id,host_id), FOREIGN KEY(host_id,owner_id) REFERENCES hosts(id,owner_id), FOREIGN KEY(predecessor_id,host_id) REFERENCES host_identities(id,host_id), CHECK(expires_at=issued_at+86400))"
    )

    execute(
      "CREATE UNIQUE INDEX one_active_host_identity ON host_identities(host_id) WHERE status='active'"
    )

    execute(
      "CREATE UNIQUE INDEX one_pending_host_identity ON host_identities(host_id) WHERE status='pending'"
    )

    execute(
      "CREATE TABLE host_connection_slots (host_id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL, connection_id TEXT, state TEXT NOT NULL CHECK(state IN ('unknown','connected')), FOREIGN KEY(host_id,owner_id) REFERENCES hosts(id,owner_id))"
    )

    execute(
      "CREATE TABLE runtime_freshness (runtime_id TEXT PRIMARY KEY NOT NULL, generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0), intent_version INTEGER NOT NULL CHECK(typeof(intent_version)='integer' AND intent_version>0), connection_id TEXT NOT NULL, FOREIGN KEY(runtime_id,generation) REFERENCES runtime_instances(id,generation))"
    )

    execute(
      "CREATE TRIGGER host_revocation_retained BEFORE UPDATE ON host_security WHEN NEW.host_id IS NOT OLD.host_id OR NEW.owner_id IS NOT OLD.owner_id OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS NOT OLD.revoked_at) BEGIN SELECT RAISE(ABORT,'host invariant'); END"
    )

    execute(
      "CREATE TRIGGER host_identity_revoked_insert BEFORE INSERT ON host_identities WHEN NEW.status IN ('active','pending') AND EXISTS(SELECT 1 FROM host_security WHERE host_id=NEW.host_id AND revoked_at IS NOT NULL) BEGIN SELECT RAISE(ABORT,'host invariant'); END"
    )

    execute(
      "CREATE TRIGGER host_identity_retained BEFORE UPDATE ON host_identities WHEN NEW.host_id IS NOT OLD.host_id OR NEW.owner_id IS NOT OLD.owner_id OR NEW.fingerprint IS NOT OLD.fingerprint OR NEW.certificate IS NOT OLD.certificate OR NEW.csr_hash IS NOT OLD.csr_hash OR NEW.predecessor_id IS NOT OLD.predecessor_id OR NEW.issued_at IS NOT OLD.issued_at OR NEW.expires_at IS NOT OLD.expires_at OR (OLD.status='revoked' AND NEW.status!='revoked') OR (OLD.status='retired' AND NEW.status NOT IN ('retired','revoked')) OR (NEW.status IN ('active','pending') AND EXISTS(SELECT 1 FROM host_security WHERE host_id=NEW.host_id AND revoked_at IS NOT NULL)) BEGIN SELECT RAISE(ABORT,'host invariant'); END"
    )

    for table <- ~w(host_security host_enrollments host_identities) do
      execute(
        "CREATE TRIGGER #{table}_retained BEFORE DELETE ON #{table} BEGIN SELECT RAISE(ABORT,'host invariant'); END"
      )
    end
  end

  def down, do: raise("identity/revocation history must not be discarded")
end
