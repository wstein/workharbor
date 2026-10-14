# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.FoundationDatabase do
  @moduledoc false
  alias WorkHarbor.Repo

  def open!(path) do
    {:ok, pid} =
      Repo.start_link(
        name: nil,
        database: path,
        pool: DBConnection.ConnectionPool,
        pool_size: 1,
        journal_mode: :wal,
        foreign_keys: :on,
        synchronous: :full,
        busy_timeout: 2000,
        log: false
      )

    Process.unlink(pid)
    Repo.put_dynamic_repo(pid)
    migration = WorkHarbor.Repo.Migrations.OwnerFoundation

    unless Code.ensure_loaded?(migration),
      do:
        Code.require_file(
          Application.app_dir(
            :workharbor,
            "priv/repo/migrations/20261014000000_owner_foundation.exs"
          )
        )

    Ecto.Migrator.run(Repo, [{20_261_014_000_000, migration}], :up, all: true, log: false)
    GenServer.stop(pid)

    {:ok, writers} =
      Repo.start_link(
        name: nil,
        database: path,
        pool: DBConnection.ConnectionPool,
        pool_size: 5,
        journal_mode: :wal,
        foreign_keys: :on,
        synchronous: :full,
        busy_timeout: 2000,
        log: false
      )

    Process.unlink(writers)
    Repo.put_dynamic_repo(writers)
    writers
  end

  def sql(statement, parameters \\ []) do
    Repo.query!(statement, parameters, log: false)
  end

  def count(table), do: sql("SELECT count(*) FROM " <> table).rows |> hd() |> hd()

  def fixture! do
    alias WorkHarbor.Foundation, as: F
    {:ok, owner} = F.create_owner("private owner name")
    {:ok, host} = F.create_host(owner.id)
    {owner.id, host.id}
  end

  def request(host, workspace \\ Ecto.UUID.generate(), action \\ "create") do
    parameters = if action == "create", do: %{"host_id" => host, "config" => config()}, else: %{}

    %{
      "schema_version" => 1,
      "workspace_id" => workspace,
      "action" => action,
      "parameters" => parameters
    }
  end

  def config(commit \\ String.duplicate("a", 40)) do
    %{
      "repository_identity" => "git@example.test:private-repository.git",
      "commit" => commit,
      "submodules" => []
    }
  end

  # A historical confirmed runtime fixture; no provider or lifecycle is invoked.
  def current!(operation) do
    alias WorkHarbor.Foundation, as: F

    candidate =
      F.bindings(
        operation.owner_id,
        sql("SELECT host_id FROM workspaces WHERE id = ?", [operation.workspace_id]).rows
        |> hd()
        |> hd(),
        operation.id
      )["candidate"]

    sql(
      "UPDATE runtime_instances SET is_current = 1, observed_state = 'running', effective_config = ? WHERE id = ?",
      [Jason.encode!(config()), candidate["id"]]
    )

    candidate
  end

  def finish!(owner, host, operation, status \\ "failed") do
    alias WorkHarbor.Foundation, as: F

    outcome =
      if status == "failed",
        do: %{"status" => status, "code" => "operation_failed"},
        else: %{"status" => status, "code" => "ok"}

    {:ok, {:finished, result}} =
      F.finish(
        owner,
        host,
        operation.id,
        operation.intent_version,
        operation.status,
        F.bindings(owner, host, operation.id),
        outcome
      )

    result
  end
end
