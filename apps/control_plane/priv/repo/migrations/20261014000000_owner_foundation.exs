# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Repo.Migrations.OwnerFoundation do
  use Ecto.Migration

  def up do
    execute("""
    CREATE TABLE owners (id TEXT PRIMARY KEY NOT NULL, display_name TEXT NOT NULL, inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL)
    """)

    execute("""
    CREATE TABLE hosts (id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL REFERENCES owners(id), identity_fingerprint TEXT,
    capabilities TEXT NOT NULL DEFAULT '[]', last_seen TEXT, lease_expires_at TEXT, inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(id,owner_id))
    """)

    execute("""
    CREATE TABLE workspaces (id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL REFERENCES owners(id), host_id TEXT NOT NULL,
    desired_state TEXT NOT NULL CHECK(desired_state IN ('running','stopped')),
    intent_version INTEGER NOT NULL DEFAULT 0 CHECK(typeof(intent_version)='integer' AND intent_version>=0),
    last_runtime_generation INTEGER NOT NULL DEFAULT 0 CHECK(typeof(last_runtime_generation)='integer' AND last_runtime_generation>=0),
    requested_config TEXT NOT NULL, inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(id,owner_id), UNIQUE(id,host_id),
    FOREIGN KEY(host_id,owner_id) REFERENCES hosts(id,owner_id))
    """)

    execute("""
    CREATE TABLE operations (id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL REFERENCES owners(id), workspace_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL, action TEXT NOT NULL CHECK(action IN ('create','start','stop','rebuild','remove_runtime')),
    request_version INTEGER NOT NULL CHECK(request_version=1), request_bytes BLOB NOT NULL, request_hash TEXT NOT NULL,
    intent_version INTEGER NOT NULL CHECK(typeof(intent_version)='integer' AND intent_version>0),
    status TEXT NOT NULL CHECK(status IN ('queued','executing','succeeded','failed')),
    execution_config_snapshot TEXT, terminal_result TEXT, inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(id,workspace_id), UNIQUE(id,intent_version), UNIQUE(owner_id,workspace_id,idempotency_key), UNIQUE(workspace_id,intent_version),
    FOREIGN KEY(workspace_id,owner_id) REFERENCES workspaces(id,owner_id),
    CHECK((status IN ('queued','executing') AND terminal_result IS NULL) OR (status IN ('succeeded','failed') AND terminal_result IS NOT NULL)))
    """)

    execute("""
    CREATE TABLE persistent_storage (id TEXT PRIMARY KEY NOT NULL, workspace_id TEXT NOT NULL, host_id TEXT NOT NULL,
    reservation_operation_id TEXT NOT NULL UNIQUE, provider_handle TEXT, deleted_at TEXT, inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY(workspace_id,host_id) REFERENCES workspaces(id,host_id),
    FOREIGN KEY(reservation_operation_id,workspace_id) REFERENCES operations(id,workspace_id))
    """)

    execute("""
    CREATE TABLE runtime_instances (id TEXT PRIMARY KEY NOT NULL, workspace_id TEXT NOT NULL, host_id TEXT NOT NULL,
    reservation_operation_id TEXT NOT NULL UNIQUE,
    generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0), provider_instance_id TEXT,
    observed_state TEXT NOT NULL CHECK(observed_state IN ('pending','provisioning','running','stopping','stopped','failed','unknown')),
    effective_config TEXT, is_current INTEGER NOT NULL DEFAULT 0 CHECK(is_current IN (0,1)), deleted_at TEXT, inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(id,workspace_id), UNIQUE(id,generation), UNIQUE(workspace_id,generation),
    FOREIGN KEY(workspace_id,host_id) REFERENCES workspaces(id,host_id),
    FOREIGN KEY(reservation_operation_id,workspace_id) REFERENCES operations(id,workspace_id),
    CHECK(deleted_at IS NULL OR is_current=0))
    """)

    execute("""
    CREATE TABLE operation_runtime_bindings (id TEXT PRIMARY KEY NOT NULL, operation_id TEXT NOT NULL, workspace_id TEXT NOT NULL,
    runtime_id TEXT NOT NULL, role TEXT NOT NULL CHECK(role IN ('target','candidate')), inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(operation_id,role),
    FOREIGN KEY(operation_id,workspace_id) REFERENCES operations(id,workspace_id),
    FOREIGN KEY(runtime_id,workspace_id) REFERENCES runtime_instances(id,workspace_id))
    """)

    execute("""
    CREATE TABLE audit_events (id TEXT PRIMARY KEY NOT NULL, owner_id TEXT NOT NULL REFERENCES owners(id), host_id TEXT,
    workspace_id TEXT, operation_id TEXT, event TEXT NOT NULL, action TEXT NOT NULL, intent_version INTEGER,
    channel TEXT NOT NULL CHECK(channel IN ('cli','browser','internal')), outcome TEXT NOT NULL, code TEXT,
    runtime_id TEXT REFERENCES runtime_instances(id), runtime_generation INTEGER, metadata TEXT NOT NULL DEFAULT '{}', inserted_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(operation_id,event), FOREIGN KEY(host_id,owner_id) REFERENCES hosts(id,owner_id),
    FOREIGN KEY(workspace_id,owner_id) REFERENCES workspaces(id,owner_id),
    FOREIGN KEY(operation_id,workspace_id) REFERENCES operations(id,workspace_id),
    FOREIGN KEY(workspace_id,host_id) REFERENCES workspaces(id,host_id),
    FOREIGN KEY(operation_id,intent_version) REFERENCES operations(id,intent_version),
    FOREIGN KEY(runtime_id,workspace_id) REFERENCES runtime_instances(id,workspace_id),
    FOREIGN KEY(runtime_id,runtime_generation) REFERENCES runtime_instances(id,generation),
    CHECK((runtime_id IS NULL AND runtime_generation IS NULL) OR (runtime_id IS NOT NULL AND runtime_generation IS NOT NULL AND workspace_id IS NOT NULL)),
    CHECK(workspace_id IS NULL OR host_id IS NOT NULL),
    CHECK(operation_id IS NULL OR (workspace_id IS NOT NULL AND host_id IS NOT NULL AND intent_version IS NOT NULL)))
    """)

    execute(
      "CREATE UNIQUE INDEX one_unfinished_operation ON operations(workspace_id) WHERE status IN ('queued','executing')"
    )

    execute(
      "CREATE UNIQUE INDEX one_current_runtime ON runtime_instances(workspace_id) WHERE is_current=1"
    )

    execute(
      "CREATE UNIQUE INDEX one_project_storage ON persistent_storage(workspace_id) WHERE deleted_at IS NULL"
    )

    execute(
      "CREATE TRIGGER workspace_scope_immutable BEFORE UPDATE ON workspaces WHEN NEW.owner_id IS NOT OLD.owner_id OR NEW.host_id IS NOT OLD.host_id BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER host_owner_immutable BEFORE UPDATE ON hosts WHEN NEW.owner_id IS NOT OLD.owner_id BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER operation_identity_immutable BEFORE UPDATE ON operations WHEN NEW.owner_id IS NOT OLD.owner_id OR NEW.workspace_id IS NOT OLD.workspace_id OR NEW.idempotency_key IS NOT OLD.idempotency_key OR NEW.action IS NOT OLD.action OR NEW.request_version IS NOT OLD.request_version OR NEW.request_bytes IS NOT OLD.request_bytes OR NEW.request_hash IS NOT OLD.request_hash OR NEW.intent_version IS NOT OLD.intent_version OR NEW.execution_config_snapshot IS NOT OLD.execution_config_snapshot BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER operation_terminal_immutable BEFORE UPDATE ON operations WHEN OLD.status IN ('succeeded','failed') BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER operation_transition BEFORE UPDATE ON operations WHEN NOT ((OLD.status='queued' AND NEW.status IN ('queued','executing','failed')) OR (OLD.status='executing' AND NEW.status IN ('executing','succeeded','failed')) OR OLD.status IN ('succeeded','failed')) BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER runtime_identity_immutable BEFORE UPDATE ON runtime_instances WHEN NEW.workspace_id IS NOT OLD.workspace_id OR NEW.host_id IS NOT OLD.host_id OR NEW.reservation_operation_id IS NOT OLD.reservation_operation_id OR NEW.generation IS NOT OLD.generation BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER runtime_provider_immutable BEFORE UPDATE ON runtime_instances WHEN OLD.provider_instance_id IS NOT NULL AND NEW.provider_instance_id IS NOT OLD.provider_instance_id BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER runtime_effective_immutable BEFORE UPDATE ON runtime_instances WHEN OLD.effective_config IS NOT NULL AND NEW.effective_config IS NOT OLD.effective_config BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER runtime_tombstone_immutable BEFORE UPDATE ON runtime_instances WHEN OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS NOT OLD.deleted_at BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER storage_identity_immutable BEFORE UPDATE ON persistent_storage WHEN NEW.workspace_id IS NOT OLD.workspace_id OR NEW.host_id IS NOT OLD.host_id OR NEW.reservation_operation_id IS NOT OLD.reservation_operation_id BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER storage_handle_immutable BEFORE UPDATE ON persistent_storage WHEN OLD.provider_handle IS NOT NULL AND NEW.provider_handle IS NOT OLD.provider_handle BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER storage_tombstone_immutable BEFORE UPDATE ON persistent_storage WHEN OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS NOT OLD.deleted_at BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER workspaces_retained BEFORE DELETE ON workspaces WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER operations_retained BEFORE DELETE ON operations WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER runtime_instances_retained BEFORE DELETE ON runtime_instances WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER persistent_storage_retained BEFORE DELETE ON persistent_storage WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER operation_runtime_bindings_retained BEFORE DELETE ON operation_runtime_bindings WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER audit_events_retained BEFORE DELETE ON audit_events WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER bindings_immutable BEFORE UPDATE ON operation_runtime_bindings WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER audit_immutable BEFORE UPDATE ON audit_events WHEN 1 BEGIN SELECT RAISE(ABORT, 'foundation invariant'); END"
    )

    execute(
      "CREATE TRIGGER candidate_binding_owner BEFORE INSERT ON operation_runtime_bindings WHEN NEW.role='candidate' AND NOT EXISTS (SELECT 1 FROM runtime_instances r WHERE r.id=NEW.runtime_id AND r.reservation_operation_id=NEW.operation_id AND r.workspace_id=NEW.workspace_id) BEGIN SELECT RAISE(ABORT, 'candidate reservation mismatch'); END"
    )
  end

  def down do
    execute("DROP TRIGGER candidate_binding_owner")
    execute("DROP TRIGGER workspace_scope_immutable")
    execute("DROP TRIGGER host_owner_immutable")
    execute("DROP TRIGGER operation_identity_immutable")
    execute("DROP TRIGGER operation_terminal_immutable")
    execute("DROP TRIGGER operation_transition")
    execute("DROP TRIGGER runtime_identity_immutable")
    execute("DROP TRIGGER runtime_provider_immutable")
    execute("DROP TRIGGER runtime_effective_immutable")
    execute("DROP TRIGGER runtime_tombstone_immutable")
    execute("DROP TRIGGER storage_identity_immutable")
    execute("DROP TRIGGER storage_handle_immutable")
    execute("DROP TRIGGER storage_tombstone_immutable")
    execute("DROP TRIGGER workspaces_retained")
    execute("DROP TRIGGER operations_retained")
    execute("DROP TRIGGER runtime_instances_retained")
    execute("DROP TRIGGER persistent_storage_retained")
    execute("DROP TRIGGER operation_runtime_bindings_retained")
    execute("DROP TRIGGER audit_events_retained")
    execute("DROP TRIGGER bindings_immutable")
    execute("DROP TRIGGER audit_immutable")
    execute("DROP TABLE audit_events")
    execute("DROP TABLE operation_runtime_bindings")
    execute("DROP TABLE runtime_instances")
    execute("DROP TABLE persistent_storage")
    execute("DROP TABLE operations")
    execute("DROP TABLE workspaces")
    execute("DROP TABLE hosts")
    execute("DROP TABLE owners")
  end
end
