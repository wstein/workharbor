# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.StorageBindingTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Foundation, Repo}
  alias WorkHarbor.FoundationDatabase, as: DB

  setup do
    path = Path.join(System.tmp_dir!(), "whr-storage-#{Ecto.UUID.generate()}.db")
    repo = DB.open!(path)
    {owner, host} = DB.fixture!()
    {:ok, {:accepted, op}} = Foundation.accept(owner, "create", DB.request(host), "cli")
    [[storage]] = DB.sql("SELECT id FROM persistent_storage").rows

    on_exit(fn ->
      if Process.alive?(repo), do: GenServer.stop(repo)
      for suffix <- ["", "-wal", "-shm"], do: File.rm(path <> suffix)
    end)

    %{repo: repo, owner: owner, host: host, op: op, storage: storage}
  end

  defp storage_binding(c, op \\ nil),
    do:
      Foundation.storage_binding(
        c.owner,
        c.host,
        (op || c.op).id,
        (op || c.op).intent_version,
        c.storage
      )

  defp observe(c, handle),
    do:
      Foundation.observe_storage(c.owner, c.host, c.op.id, c.op.intent_version, c.storage, handle)

  test "binding carries original create descriptor and reservation", c do
    assert {:ok, b} = storage_binding(c)

    assert b == %{
             storage_id: c.storage,
             owner_id: c.owner,
             host_id: c.host,
             workspace_id: c.op.workspace_id,
             reservation_operation_id: c.op.id,
             initial_config: DB.config(),
             provider_handle: nil
           }
  end

  test "first handle requires executing current scope and repeats immutably", c do
    assert {:error, :invalid_transition} = observe(c, "host-private-handle")
    assert {:ok, {:claimed, _}} = Foundation.claim(c.owner, c.host, c.op.id, 1)
    assert {:ok, b} = observe(c, "host-private-handle")
    assert b.provider_handle == "host-private-handle"
    assert {:ok, ^b} = observe(c, "host-private-handle")
    assert {:error, :storage_mismatch} = observe(c, "different-handle")
    assert {:error, :invalid_request} = observe(c, "")
    assert {:ok, ^b} = storage_binding(c)
  end

  test "foreign operation storage host owner and stale intent refuse", c do
    for {owner, host, op, intent, storage} <- [
          {Ecto.UUID.generate(), c.host, c.op.id, 1, c.storage},
          {c.owner, Ecto.UUID.generate(), c.op.id, 1, c.storage},
          {c.owner, c.host, Ecto.UUID.generate(), 1, c.storage},
          {c.owner, c.host, c.op.id, 1, Ecto.UUID.generate()},
          {c.owner, c.host, c.op.id, 0, c.storage}
        ] do
      assert {:error, code} = Foundation.storage_binding(owner, host, op, intent, storage)

      assert {:error, other} =
               Foundation.observe_storage(owner, host, op, intent, storage, "handle")

      assert code in [:not_found, :storage_unavailable, :stale_intent]
      assert other in [:not_found, :invalid_transition, :stale_intent]
    end

    assert [[nil]] = DB.sql("SELECT provider_handle FROM persistent_storage").rows
  end

  test "later approved revision retains original seed and handle", c do
    {:ok, {:claimed, _}} = Foundation.claim(c.owner, c.host, c.op.id, 1)
    assert {:ok, _} = observe(c, "retained")
    DB.finish!(c.owner, c.host, %{c.op | status: "executing"})

    request = %{
      DB.request(c.host, c.op.workspace_id, "rebuild")
      | "parameters" => %{"config" => DB.config(String.duplicate("b", 40))}
    }

    {:ok, {:accepted, next}} = Foundation.accept(c.owner, "rebuild", request, "cli")
    assert next.execution_config_snapshot != DB.config()
    assert {:ok, b} = storage_binding(c, next)
    assert b.initial_config == DB.config()
    assert b.reservation_operation_id == c.op.id
    assert b.provider_handle == "retained"
    assert {:error, :stale_intent} = storage_binding(c)
  end

  test "independent competing handles have one winner and never replace", c do
    {:ok, {:claimed, _}} = Foundation.claim(c.owner, c.host, c.op.id, 1)

    results =
      for handle <- ["one", "two"] do
        Task.async(fn ->
          Repo.put_dynamic_repo(c.repo)
          observe(c, handle)
        end)
      end
      |> Enum.map(&Task.await(&1, 5000))

    assert Enum.count(results, &match?({:ok, _}, &1)) == 1
    assert Enum.count(results, &(&1 == {:error, :storage_mismatch})) == 1
  end
end
