# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Foundation.Owner do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "owners" do
    field :display_name, :string
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.Host do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "hosts" do
    field :owner_id, :binary_id
    field :identity_fingerprint, :string
    field :capabilities, {:array, :string}, default: []
    field :last_seen, :utc_datetime_usec
    field :lease_expires_at, :utc_datetime_usec
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.Workspace do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "workspaces" do
    field :owner_id, :binary_id
    field :host_id, :binary_id
    field :desired_state, :string
    field :intent_version, :integer, default: 0
    field :last_runtime_generation, :integer, default: 0
    field :requested_config, :map
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.Operation do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "operations" do
    field :owner_id, :binary_id
    field :workspace_id, :binary_id
    field :idempotency_key, :string
    field :action, :string
    field :request_version, :integer
    field :request_bytes, :binary
    field :request_hash, :string
    field :intent_version, :integer
    field :status, :string
    field :execution_config_snapshot, :map
    field :terminal_result, :map
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.PersistentStorage do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "persistent_storage" do
    field :workspace_id, :binary_id
    field :host_id, :binary_id
    field :reservation_operation_id, :binary_id
    field :provider_handle, :string
    field :deleted_at, :utc_datetime_usec
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.RuntimeInstance do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "runtime_instances" do
    field :workspace_id, :binary_id
    field :host_id, :binary_id
    field :reservation_operation_id, :binary_id
    field :generation, :integer
    field :provider_instance_id, :string
    field :observed_state, :string
    field :effective_config, :map
    field :is_current, :boolean, default: false
    field :deleted_at, :utc_datetime_usec
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.RuntimeBinding do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "operation_runtime_bindings" do
    field :operation_id, :binary_id
    field :workspace_id, :binary_id
    field :runtime_id, :binary_id
    field :role, :string
    timestamps()
  end
end

defmodule WorkHarbor.Foundation.AuditEvent do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "audit_events" do
    field :owner_id, :binary_id
    field :host_id, :binary_id
    field :workspace_id, :binary_id
    field :operation_id, :binary_id
    field :event, :string
    field :action, :string
    field :intent_version, :integer
    field :channel, :string
    field :outcome, :string
    field :code, :string
    field :runtime_id, :binary_id
    field :runtime_generation, :integer
    field :metadata, :map, default: %{}
    timestamps()
  end
end
