# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.FoundationTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.Foundation, as: F
  alias WorkHarbor.FoundationDatabase, as: DB
  alias WorkHarbor.Repo

  setup do
    path = Path.join(System.tmp_dir!(), "workharbor-foundation-#{Ecto.UUID.generate()}.db")
    pid = DB.open!(path)
    {owner, host} = DB.fixture!()

    on_exit(fn ->
      if Process.alive?(pid), do: GenServer.stop(pid)
      for suffix <- ["", "-wal", "-shm"], do: File.rm(path <> suffix)
    end)

    %{path: path, pid: pid, owner: owner, host: host}
  end

  test "acceptance commits scoped intent reservations and redacted audit atomically", c do
    assert {:ok, {:accepted, operation}} = F.accept(c.owner, "key", DB.request(c.host), "cli")
    assert operation.status == "queued"
    assert operation.intent_version == 1
    assert DB.count("persistent_storage") == 1
    assert DB.count("runtime_instances") == 1
    bindings = F.bindings(c.owner, c.host, operation.id)
    assert bindings["candidate"]["generation"] == 1
    assert bindings["target"] == nil

    assert DB.sql(
             "SELECT observed_state, provider_instance_id, effective_config, is_current FROM runtime_instances"
           ).rows == [["pending", nil, nil, 0]]

    assert DB.sql("SELECT desired_state, intent_version, last_runtime_generation FROM workspaces").rows ==
             [["running", 1, 1]]

    assert DB.sql("SELECT event FROM audit_events WHERE operation_id = ? ORDER BY event", [
             operation.id
           ]).rows == [["operation_accepted"], ["runtime_reserved"]]

    audit = DB.sql("SELECT metadata FROM audit_events").rows |> inspect()
    refute audit =~ "private owner"
    refute audit =~ "private-repository"
    refute audit =~ "repository_identity"
    assert {:error, :invalid_channel} = F.accept(c.owner, "other", DB.request(c.host), "canary")
  end

  test "equal key replay precedes busy and state checks and changed payload conflicts", c do
    request = DB.request(c.host)
    {:ok, {:accepted, operation}} = F.accept(c.owner, "key", request, "cli")
    assert {:ok, {:replayed, replay}} = F.accept(c.owner, "key", request, "browser")
    assert replay.id == operation.id
    changed = put_in(request, ["parameters", "config", "commit"], String.duplicate("b", 40))
    assert {:error, :idempotency_conflict} = F.accept(c.owner, "key", changed, "cli")

    assert {:error, :workspace_busy} =
             F.accept(c.owner, "new", %{request | "action" => "stop", "parameters" => %{}}, "cli")

    assert DB.sql("SELECT intent_version FROM workspaces").rows == [[1]]
    DB.finish!(c.owner, c.host, operation)
    assert {:ok, {:replayed, terminal}} = F.accept(c.owner, "key", request, "cli")
    assert terminal.status == "failed"
    assert DB.count("operations") == 1
    assert DB.count("runtime_instances") == 1

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'operation_accepted'").rows ==
             [[1]]
  end

  test "owner workspace key uniqueness and scope do not depend on repository owner", c do
    {foreign, foreign_host} = DB.fixture!()
    request = DB.request(c.host)
    {:ok, {:accepted, operation}} = F.accept(c.owner, "same", request, "cli")

    assert {:error, :not_found} =
             F.accept(
               foreign,
               "same",
               %{request | "action" => "stop", "parameters" => %{}},
               "cli"
             )

    assert {:error, :not_found} =
             F.accept(c.owner, "foreign-host", DB.request(foreign_host), "cli")

    assert {:error, :not_found} = F.claim(foreign, c.host, operation.id, 1)
    assert {:error, :not_found} = F.claim(c.owner, foreign_host, operation.id, 1)

    assert {:ok, {:accepted, other_workspace}} =
             F.accept(c.owner, "same", DB.request(c.host), "cli")

    refute other_workspace.id == operation.id
    assert {:ok, {:accepted, _}} = F.accept(foreign, "same", DB.request(foreign_host), "cli")
  end

  test "direct SQL constraints enforce scope active operation generations and retention", c do
    {foreign, foreign_host} = DB.fixture!()
    {:ok, {:accepted, operation}} = F.accept(c.owner, "key", DB.request(c.host), "cli")
    runtime = F.bindings(c.owner, c.host, operation.id)["candidate"]

    assert_raise Exqlite.Error, fn ->
      DB.sql("UPDATE workspaces SET owner_id = ? WHERE id = ?", [foreign, operation.workspace_id])
    end

    assert_raise Exqlite.Error, fn ->
      DB.sql("UPDATE persistent_storage SET host_id = ?", [foreign_host])
    end

    assert_raise Exqlite.Error, fn ->
      DB.sql("UPDATE runtime_instances SET host_id = ?", [foreign_host])
    end

    assert_raise Exqlite.Error, fn -> DB.sql("UPDATE operations SET owner_id = ?", [foreign]) end
    assert_raise Exqlite.Error, fn -> DB.sql("UPDATE runtime_instances SET generation = 0") end
    assert_raise Exqlite.Error, fn -> DB.sql("UPDATE workspaces SET intent_version = -1") end

    assert_raise Exqlite.Error, fn ->
      DB.sql(
        "INSERT INTO operations SELECT ?, owner_id, workspace_id, 'different', action, request_version, request_bytes, request_hash, intent_version + 1, status, execution_config_snapshot, terminal_result, inserted_at, updated_at FROM operations WHERE id = ?",
        [Ecto.UUID.generate(), operation.id]
      )
    end

    assert_raise Exqlite.Error, fn ->
      DB.sql("DELETE FROM workspaces WHERE id = ?", [operation.workspace_id])
    end

    assert_raise Exqlite.Error, fn -> DB.sql("DELETE FROM owners WHERE id = ?", [c.owner]) end

    assert DB.sql("SELECT generation FROM runtime_instances WHERE id = ?", [runtime["id"]]).rows ==
             [[1]]
  end

  test "audit failure rolls back all accepted graph and counter writes", c do
    for event <- ["operation_accepted", "runtime_reserved"] do
      DB.sql(
        "CREATE TRIGGER reject_#{event} BEFORE INSERT ON audit_events WHEN NEW.event = '#{event}' BEGIN SELECT RAISE(ABORT, 'audit rejected'); END"
      )

      assert {:error, :persistence_failed} = F.accept(c.owner, "key", DB.request(c.host), "cli")

      for table <- [
            "workspaces",
            "operations",
            "persistent_storage",
            "runtime_instances",
            "operation_runtime_bindings"
          ],
          do: assert(DB.count(table) == 0)

      DB.sql("DROP TRIGGER reject_#{event}")
    end
  end

  test "start snapshots existing effective config and never allocates replacement", c do
    request = DB.request(c.host)
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", request, "cli")
    DB.finish!(c.owner, c.host, create)
    start = DB.request(c.host, create.workspace_id, "start")
    assert {:error, :runtime_unavailable} = F.accept(c.owner, "start", start, "cli")
    target = DB.current!(create)
    {:ok, {:accepted, operation}} = F.accept(c.owner, "start", start, "cli")
    assert operation.execution_config_snapshot == DB.config()
    assert F.bindings(c.owner, c.host, operation.id) == %{"target" => target, "candidate" => nil}

    assert {:error, :invalid_action} =
             F.reserve_candidate(c.owner, c.host, operation.id, operation.intent_version)

    assert DB.sql("SELECT intent_version, last_runtime_generation FROM workspaces").rows == [
             [2, 1]
           ]

    assert DB.count("runtime_instances") == 1
  end

  test "rebuild requested config never relabels existing effective config", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    old = DB.current!(create)
    DB.finish!(c.owner, c.host, create)
    new_config = DB.config(String.duplicate("b", 40))

    request = %{
      DB.request(c.host, create.workspace_id, "rebuild")
      | "parameters" => %{"config" => new_config}
    }

    {:ok, {:accepted, rebuild}} = F.accept(c.owner, "rebuild", request, "cli")
    candidate = F.bindings(c.owner, c.host, rebuild.id)["candidate"]
    assert candidate["generation"] == 2

    assert {:ok, {:replayed, reserved}} =
             F.reserve_candidate(c.owner, c.host, rebuild.id, rebuild.intent_version)

    assert reserved.id == candidate["id"]

    assert DB.sql("SELECT effective_config FROM runtime_instances WHERE id = ?", [old["id"]]).rows ==
             [[Jason.encode!(DB.config())]]

    assert DB.sql("SELECT effective_config FROM runtime_instances WHERE id = ?", [candidate["id"]]).rows ==
             [[nil]]

    DB.finish!(c.owner, c.host, rebuild)

    assert DB.sql("SELECT effective_config FROM runtime_instances WHERE id = ?", [old["id"]]).rows ==
             [[Jason.encode!(DB.config())]]

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'runtime_reserved'").rows == [
             [2]
           ]

    assert DB.count("persistent_storage") == 1
  end

  test "claim and results bind current intent exact runtime and immutable terminal outcome", c do
    {:ok, {:accepted, operation}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    bindings = F.bindings(c.owner, c.host, operation.id)
    success = %{"status" => "succeeded", "code" => "ok"}
    failed = %{"status" => "failed", "code" => "operation_failed"}

    assert {:error, :invalid_transition} =
             F.finish(c.owner, c.host, operation.id, 1, "queued", bindings, success)

    assert {:error, :stale_intent} = F.claim(c.owner, c.host, operation.id, 2)
    assert {:ok, {:claimed, executing}} = F.claim(c.owner, c.host, operation.id, 1)
    assert executing.status == "executing"
    assert {:ok, {:already_executing, _}} = F.claim(c.owner, c.host, operation.id, 1)
    bad = put_in(bindings, ["candidate", "generation"], 99)

    assert {:error, :runtime_mismatch} =
             F.finish(c.owner, c.host, operation.id, 1, "executing", bad, success)

    assert {:ok, {:finished, terminal}} =
             F.finish(c.owner, c.host, operation.id, 1, "executing", bindings, success)

    assert {:ok, {:replayed, replay}} =
             F.finish(c.owner, c.host, operation.id, 1, "executing", bindings, success)

    assert replay.terminal_result == terminal.terminal_result

    assert {:error, :result_conflict} =
             F.finish(c.owner, c.host, operation.id, 1, "executing", bindings, failed)

    assert {:error, :invalid_transition} = F.claim(c.owner, c.host, operation.id, 1)

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'operation_claimed'").rows == [
             [1]
           ]

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'operation_terminal'").rows ==
             [[1]]
  end

  test "same runtime generation cannot authorize stale operation results", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    DB.current!(create)
    DB.finish!(c.owner, c.host, create)

    {:ok, {:accepted, stop}} =
      F.accept(c.owner, "stop", DB.request(c.host, create.workspace_id, "stop"), "cli")

    {:ok, {:claimed, _}} = F.claim(c.owner, c.host, stop.id, stop.intent_version)

    DB.sql("UPDATE workspaces SET intent_version = intent_version + 1 WHERE id = ?", [
      create.workspace_id
    ])

    assert {:error, :stale_intent} =
             F.finish(
               c.owner,
               c.host,
               stop.id,
               stop.intent_version,
               "executing",
               F.bindings(c.owner, c.host, stop.id),
               %{"status" => "succeeded", "code" => "ok"}
             )

    assert DB.sql("SELECT status FROM operations WHERE id = ?", [stop.id]).rows == [["executing"]]
    assert DB.sql("SELECT last_runtime_generation FROM workspaces").rows == [[1]]
  end

  test "confirmed runtime tombstone retains storage key history and terminal replay", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    target = DB.current!(create)
    DB.finish!(c.owner, c.host, create)
    request = DB.request(c.host, create.workspace_id, "remove_runtime")
    {:ok, {:accepted, remove}} = F.accept(c.owner, "remove", request, "cli")

    assert DB.sql("SELECT deleted_at FROM runtime_instances WHERE id = ?", [target["id"]]).rows ==
             [[nil]]

    {:ok, {:claimed, _}} = F.claim(c.owner, c.host, remove.id, remove.intent_version)

    observation = %{
      "runtime_id" => target["id"],
      "generation" => target["generation"],
      "observed_state" => "stopped",
      "deleted" => true
    }

    args = [
      c.owner,
      c.host,
      remove.id,
      remove.intent_version,
      "executing",
      F.bindings(c.owner, c.host, remove.id),
      %{"status" => "succeeded", "code" => "ok"},
      observation
    ]

    assert {:ok, {:finished, _}} = apply(F, :finish, args)

    assert DB.sql(
             "SELECT deleted_at IS NOT NULL, is_current FROM runtime_instances WHERE id = ?",
             [target["id"]]
           ).rows == [[1, 0]]

    assert DB.count("persistent_storage") == 1
    assert DB.count("workspaces") == 1
    assert {:ok, {:replayed, _}} = F.accept(c.owner, "remove", request, "browser")

    {:ok, {:accepted, newer}} =
      F.accept(c.owner, "later-stop", DB.request(c.host, create.workspace_id, "stop"), "cli")

    assert newer.intent_version == 3
    assert {:ok, {:replayed, _}} = apply(F, :finish, args)

    assert {:error, :invalid_request} =
             F.accept(c.owner, "data", %{request | "action" => "delete_data"}, "cli")
  end

  test "independent writers serialize equal changed and competing submissions", c do
    request = DB.request(c.host)

    results =
      race(c.pid, fn index ->
        F.accept(c.owner, "equal", request, if(index == 1, do: "cli", else: "browser"))
      end)

    assert Enum.sort(Enum.map(results, fn {:ok, {kind, _}} -> kind end)) == [:accepted, :replayed]
    assert results |> Enum.map(fn {:ok, {_, op}} -> op.id end) |> Enum.uniq() |> length() == 1
    assert DB.count("runtime_instances") == 1
    assert DB.count("operations") == 1
    {_, {_, operation}} = hd(results)
    DB.finish!(c.owner, c.host, operation)
    other = DB.request(c.host)
    changed = put_in(other, ["parameters", "config", "commit"], String.duplicate("b", 40))

    results =
      race(c.pid, fn index ->
        F.accept(c.owner, "changed", if(index == 1, do: other, else: changed), "cli")
      end)

    assert Enum.count(results, &match?({:ok, {:accepted, _}}, &1)) == 1
    assert {:error, :idempotency_conflict} in results
    fresh = DB.request(c.host)
    results = race(c.pid, fn index -> F.accept(c.owner, "different-#{index}", fresh, "cli") end)
    assert Enum.count(results, &match?({:ok, {:accepted, _}}, &1)) == 1
    assert {:error, :workspace_busy} in results
    assert DB.count("operations") == 3
  end

  test "every independent writer uses foreign keys full synchronous and WAL", c do
    parent = self()

    tasks =
      for _ <- 1..5 do
        Task.async(fn ->
          Repo.put_dynamic_repo(c.pid)

          Repo.checkout(fn ->
            send(parent, :checked_out)

            receive do
              :measure -> :ok
            end

            for pragma <- ["foreign_keys", "synchronous", "journal_mode"],
                do: DB.sql("PRAGMA " <> pragma).rows
          end)
        end)
      end

    for _ <- tasks, do: assert_receive(:checked_out, 5000)
    for task <- tasks, do: send(task.pid, :measure)
    for task <- tasks, do: assert(Task.await(task) == [[[1]], [[2]], [["wal"]]])
  end

  test "acceptance rejects nested transactions before any writes", c do
    assert {:ok, {:error, :nested_transaction}} =
             Repo.transaction(
               fn ->
                 F.accept(c.owner, "nested", DB.request(c.host), "cli")
               end,
               mode: :immediate
             )

    assert DB.count("operations") == 0
    assert DB.count("workspaces") == 0
  end

  test "process crash before commit rolls back and after commit survives reopen", c do
    for phase <- ["before", "after", "claimed"] do
      path = c.path <> ".#{phase}"
      pid = DB.open!(path)
      {owner, host} = DB.fixture!()
      GenServer.stop(pid)
      Repo.put_dynamic_repo(c.pid)
      workspace = Ecto.UUID.generate()

      code = """
      Logger.configure(level: :error)
      {:ok, _} = Application.ensure_all_started(:ecto_sqlite3)
      {:ok, _} = Application.ensure_all_started(:ecto_sql)
      {:ok, repo} = WorkHarbor.Repo.start_link(name: nil, database: #{inspect(path)}, pool: DBConnection.ConnectionPool, synchronous: :full, foreign_keys: :on, journal_mode: :wal, log: false)
      WorkHarbor.Repo.put_dynamic_repo(repo)
      work = fn ->
        {:ok, {:accepted, _}} = WorkHarbor.Foundation.accept(#{inspect(owner)}, "crash-key", #{inspect(DB.request(host, workspace))}, "cli")
      end
      wait = fn -> IO.puts("T005_READY"); Process.sleep(:infinity) end
      if #{inspect(phase)} == "before" do
        :telemetry.attach("crash-before-commit", [:work_harbor, :repo, :query], fn _, _, metadata, _ ->
          if String.starts_with?(metadata.query, ~s(INSERT INTO "audit_events")) and match?({:ok, %{num_rows: 1}}, metadata.result) and "operation_accepted" in metadata.params do
            IO.puts("T005_READY")
            Process.sleep(:infinity)
          end
        end, nil)
        work.()
      else
        work.()
        if #{inspect(phase)} == "claimed" do
          {:ok, operation} = WorkHarbor.Foundation.lookup(#{inspect(owner)}, #{inspect(workspace)}, "crash-key")
          {:ok, {:claimed, _}} = WorkHarbor.Foundation.claim(#{inspect(owner)}, #{inspect(host)}, operation.id, operation.intent_version)
        end
        wait.()
      end
      """

      args =
        Enum.flat_map(:code.get_path(), fn path -> ["-pa", List.to_string(path)] end) ++
          ["-e", code]

      port =
        Port.open({:spawn_executable, System.find_executable("elixir")}, [
          :binary,
          :exit_status,
          :stderr_to_stdout,
          args: args
        ])

      try do
        wait_ready(port, "")
        {:os_pid, os_pid} = Port.info(port, :os_pid)
        assert {_, 0} = System.cmd("kill", ["-KILL", Integer.to_string(os_pid)])
        assert_receive {^port, {:exit_status, _}}, 10_000
        reopened = DB.open!(path)

        try do
          if phase == "before" do
            for table <- [
                  "workspaces",
                  "operations",
                  "persistent_storage",
                  "runtime_instances",
                  "operation_runtime_bindings"
                ],
                do: assert(DB.count(table) == 0)
          else
            assert DB.count("operations") == 1

            assert {:ok, {:replayed, operation}} =
                     F.accept(owner, "crash-key", DB.request(host, workspace), "cli")

            assert operation.status == if(phase == "claimed", do: "executing", else: "queued")
            assert DB.count("persistent_storage") == 1
            assert DB.count("runtime_instances") == 1

            assert DB.sql("SELECT count(*) FROM audit_events WHERE operation_id IS NOT NULL").rows ==
                     [[if(phase == "claimed", do: 3, else: 2)]]
          end
        after
          GenServer.stop(reopened)
          Repo.put_dynamic_repo(c.pid)
        end
      after
        case Port.info(port, :os_pid) do
          {:os_pid, alive} ->
            System.cmd("kill", ["-KILL", Integer.to_string(alive)], stderr_to_stdout: true)

            receive do
              {^port, {:exit_status, _}} -> :ok
            after
              1000 -> :ok
            end

          _ ->
            :ok
        end

        for suffix <- ["", "-wal", "-shm"], do: File.rm(path <> suffix)
      end
    end
  end

  test "target observations cannot initially populate effective config", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    target = F.bindings(c.owner, c.host, create.id)["candidate"]
    DB.sql("UPDATE runtime_instances SET is_current = 1 WHERE id = ?", [target["id"]])
    DB.finish!(c.owner, c.host, create)

    for action <- ["stop", "remove_runtime"] do
      {:ok, {:accepted, operation}} =
        F.accept(c.owner, action, DB.request(c.host, create.workspace_id, action), "cli")

      {:ok, {:claimed, _}} = F.claim(c.owner, c.host, operation.id, operation.intent_version)

      observation = %{
        "runtime_id" => target["id"],
        "generation" => 1,
        "observed_state" => "stopped",
        "effective_config" => DB.config()
      }

      assert {:error, :runtime_mismatch} =
               F.finish(
                 c.owner,
                 c.host,
                 operation.id,
                 operation.intent_version,
                 "executing",
                 F.bindings(c.owner, c.host, operation.id),
                 %{"status" => "succeeded", "code" => "ok"},
                 observation
               )

      assert DB.sql(
               "SELECT effective_config, observed_state FROM runtime_instances WHERE id = ?",
               [target["id"]]
             ).rows == [[nil, "pending"]]

      assert DB.sql("SELECT status, terminal_result FROM operations WHERE id = ?", [operation.id]).rows ==
               [["executing", nil]]

      assert DB.sql(
               "SELECT count(*) FROM audit_events WHERE operation_id = ? AND event = 'operation_terminal'",
               [operation.id]
             ).rows == [[0]]

      DB.finish!(c.owner, c.host, %{operation | status: "executing"})
    end
  end

  test "candidate binding cannot borrow another operation runtime", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    target = DB.current!(create)
    DB.finish!(c.owner, c.host, create)

    {:ok, {:accepted, stop}} =
      F.accept(c.owner, "stop", DB.request(c.host, create.workspace_id, "stop"), "cli")

    assert_raise Exqlite.Error, fn ->
      DB.sql(
        "INSERT INTO operation_runtime_bindings (id, operation_id, workspace_id, runtime_id, role, inserted_at, updated_at) VALUES (?, ?, ?, ?, 'candidate', ?, ?)",
        [
          Ecto.UUID.generate(),
          stop.id,
          stop.workspace_id,
          target["id"],
          DateTime.to_iso8601(DateTime.utc_now()),
          DateTime.to_iso8601(DateTime.utc_now())
        ]
      )
    end
  end

  test "insert scope negatives reach foreign keys without update trigger masking", c do
    {foreign, foreign_host} = DB.fixture!()
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")

    assert_raise Exqlite.Error, ~r/FOREIGN KEY constraint failed/, fn ->
      DB.sql(
        "INSERT INTO workspaces SELECT ?, ?, host_id, desired_state, 0, 0, requested_config, inserted_at, updated_at FROM workspaces WHERE id = ?",
        [Ecto.UUID.generate(), foreign, create.workspace_id]
      )
    end

    assert_raise Exqlite.Error, ~r/FOREIGN KEY constraint failed/, fn ->
      clone_operation!(create, owner: foreign)
    end

    reservation = clone_operation!(create)

    assert_raise Exqlite.Error, ~r/FOREIGN KEY constraint failed/, fn ->
      DB.sql(
        "INSERT INTO runtime_instances SELECT ?, workspace_id, ?, ?, generation+1, NULL, 'pending', NULL, 0, NULL, inserted_at, updated_at FROM runtime_instances WHERE reservation_operation_id = ?",
        [Ecto.UUID.generate(), foreign_host, reservation, create.id]
      )
    end

    fresh = Ecto.UUID.generate()

    DB.sql(
      "INSERT INTO workspaces SELECT ?, owner_id, host_id, 'stopped', 0, 0, requested_config, inserted_at, updated_at FROM workspaces WHERE id = ?",
      [fresh, create.workspace_id]
    )

    fresh_op = clone_operation!(create, workspace: fresh, version: 1)

    assert_raise Exqlite.Error, ~r/FOREIGN KEY constraint failed/, fn ->
      DB.sql(
        "INSERT INTO persistent_storage SELECT ?, ?, ?, ?, NULL, NULL, inserted_at, updated_at FROM persistent_storage WHERE workspace_id = ?",
        [Ecto.UUID.generate(), fresh, foreign_host, fresh_op, create.workspace_id]
      )
    end

    assert DB.sql("PRAGMA foreign_key_check").rows == []
  end

  test "direct operation key and current runtime constraints remain enforced", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")

    assert_raise Exqlite.Error, ~r/UNIQUE constraint failed: operations.owner_id/, fn ->
      clone_operation!(create, key: "create")
    end

    old = DB.current!(create)
    DB.finish!(c.owner, c.host, create)

    rebuild_request = %{
      DB.request(c.host, create.workspace_id, "rebuild")
      | "parameters" => %{"config" => DB.config()}
    }

    {:ok, {:accepted, rebuild}} = F.accept(c.owner, "rebuild", rebuild_request, "cli")
    candidate = F.bindings(c.owner, c.host, rebuild.id)["candidate"]

    assert_raise Exqlite.Error, ~r/UNIQUE constraint failed/, fn ->
      DB.sql("UPDATE runtime_instances SET is_current = 1 WHERE id = ?", [candidate["id"]])
    end

    assert DB.sql("SELECT id FROM runtime_instances WHERE is_current = 1").rows == [[old["id"]]]

    assert_raise Exqlite.Error, fn ->
      DB.sql("DELETE FROM operations WHERE id = ?", [create.id])
    end

    assert_raise Exqlite.Error, fn -> DB.sql("DELETE FROM persistent_storage") end
  end

  test "candidate effective config and terminal observation replay are immutable", c do
    {:ok, {:accepted, operation}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    {:ok, {:claimed, _}} = F.claim(c.owner, c.host, operation.id, 1)
    candidate = F.bindings(c.owner, c.host, operation.id)["candidate"]

    observation = %{
      "runtime_id" => candidate["id"],
      "generation" => 1,
      "observed_state" => "running",
      "provider_instance_id" => "opaque-private-handle",
      "effective_config" => DB.config()
    }

    bad = %{observation | "effective_config" => DB.config(String.duplicate("b", 40))}
    success = %{"status" => "succeeded", "code" => "ok"}
    bindings = F.bindings(c.owner, c.host, operation.id)

    assert {:error, :runtime_mismatch} =
             F.finish(c.owner, c.host, operation.id, 1, "executing", bindings, success, bad)

    assert DB.sql("SELECT effective_config, provider_instance_id FROM runtime_instances").rows ==
             [[nil, nil]]

    assert {:ok, {:finished, _}} =
             F.finish(
               c.owner,
               c.host,
               operation.id,
               1,
               "executing",
               bindings,
               success,
               observation
             )

    assert {:ok, {:replayed, _}} =
             F.finish(
               c.owner,
               c.host,
               operation.id,
               1,
               "executing",
               bindings,
               success,
               observation
             )

    assert {:error, :result_conflict} =
             F.finish(c.owner, c.host, operation.id, 1, "executing", bindings, success, %{
               observation
               | "provider_instance_id" => "changed"
             })

    audit = DB.sql("SELECT metadata, code FROM audit_events").rows |> inspect()
    refute audit =~ "opaque-private-handle"
    refute audit =~ "private-repository"
    assert DB.sql("SELECT is_current FROM runtime_instances").rows == [[0]]
  end

  test "later candidate reservation has atomic audit and replay allocates nothing", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    DB.finish!(c.owner, c.host, create)
    # Committed unreserved intent fixture exercises the guarded later-reservation API.
    operation_id = clone_operation!(create, status: "queued")
    DB.sql("UPDATE workspaces SET intent_version = 2 WHERE id = ?", [create.workspace_id])

    DB.sql(
      "CREATE TRIGGER fail_later_audit BEFORE INSERT ON audit_events WHEN NEW.event = 'runtime_reserved' BEGIN SELECT RAISE(ABORT, 'audit rejected'); END"
    )

    assert {:error, :persistence_failed} = F.reserve_candidate(c.owner, c.host, operation_id, 2)
    assert DB.sql("SELECT last_runtime_generation FROM workspaces").rows == [[1]]
    assert DB.count("runtime_instances") == 1
    DB.sql("DROP TRIGGER fail_later_audit")
    assert {:ok, {:reserved, runtime}} = F.reserve_candidate(c.owner, c.host, operation_id, 2)
    assert runtime.generation == 2
    assert {:ok, {:replayed, replay}} = F.reserve_candidate(c.owner, c.host, operation_id, 2)
    assert replay.id == runtime.id

    assert DB.sql(
             "SELECT count(*) FROM audit_events WHERE operation_id = ? AND event = 'runtime_reserved'",
             [operation_id]
           ).rows == [[1]]
  end

  test "writer lock contention is bounded and never falsely accepted", c do
    parent = self()

    holder =
      Task.async(fn ->
        Repo.put_dynamic_repo(c.pid)

        Repo.transaction(
          fn ->
            send(parent, :holding_write_lock)

            receive do
              :release -> :ok
            end
          end,
          mode: :immediate
        )
      end)

    assert_receive :holding_write_lock, 5000
    start = System.monotonic_time(:millisecond)

    try do
      assert {:error, :persistence_busy} = F.accept(c.owner, "locked", DB.request(c.host), "cli")
      assert System.monotonic_time(:millisecond) - start < 5000
    after
      send(holder.pid, :release)
      Task.await(holder)
    end

    assert DB.count("operations") == 0
  end

  test "binding generations require exact integer types and complete role shape", c do
    {:ok, {:accepted, operation}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    {:ok, {:claimed, _}} = F.claim(c.owner, c.host, operation.id, 1)
    bindings = F.bindings(c.owner, c.host, operation.id)

    bad = [
      put_in(bindings, ["candidate", "generation"], 1.0),
      Map.put(bindings, "extra", nil),
      Map.delete(bindings, "target"),
      %{"target" => bindings["candidate"], "candidate" => nil}
    ]

    for malformed <- bad do
      assert {:error, :runtime_mismatch} =
               F.finish(c.owner, c.host, operation.id, 1, "executing", malformed, %{
                 "status" => "succeeded",
                 "code" => "ok"
               })

      assert DB.sql("SELECT status FROM operations WHERE id = ?", [operation.id]).rows == [
               ["executing"]
             ]

      assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'operation_terminal'").rows ==
               [[0]]
    end
  end

  test "lookup rejects malformed keys without query exceptions or data disclosure", c do
    {:ok, {:accepted, operation}} = F.accept(c.owner, "key", DB.request(c.host), "cli")

    for key <- [nil, %{"secret" => "canary"}, 1, "", "é", String.duplicate("x", 129)] do
      assert {:error, :not_found} = F.lookup(c.owner, operation.workspace_id, key)
    end

    assert {:ok, found} = F.lookup(c.owner, operation.workspace_id, "key")
    assert found.id == operation.id
  end

  test "audit identity references cannot mismatch host runtime or intent scope", c do
    {:ok, other_host} = F.create_host(c.owner)
    {:ok, {:accepted, operation}} = F.accept(c.owner, "one", DB.request(c.host), "cli")
    {:ok, {:accepted, other}} = F.accept(c.owner, "two", DB.request(c.host), "cli")
    foreign_runtime = F.bindings(c.owner, c.host, other.id)["candidate"]

    sql =
      "INSERT INTO audit_events SELECT ?, owner_id, ?, workspace_id, operation_id, 'operation_claimed', action, ?, 'internal', 'executing', NULL, ?, ?, '{}', inserted_at, updated_at FROM audit_events WHERE operation_id = ? AND event = 'operation_accepted'"

    variants = [
      [other_host.id, 1, nil, nil],
      [c.host, 1, foreign_runtime["id"], 1],
      [c.host, 99, nil, nil],
      [c.host, 1, F.bindings(c.owner, c.host, operation.id)["candidate"]["id"], 99]
    ]

    for parts <- variants do
      assert_raise Exqlite.Error, ~r/FOREIGN KEY constraint failed/, fn ->
        DB.sql(sql, [Ecto.UUID.generate()] ++ parts ++ [operation.id])
      end
    end
  end

  test "native adapter errors and audit never expose request canaries", c do
    request =
      put_in(
        DB.request(c.host),
        ["parameters", "config", "repository_identity"],
        "git@example.test:private-canary-value.git"
      )

    log =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        {:ok, {:accepted, _}} = F.accept(c.owner, "key", request, "cli")

        DB.sql(
          "CREATE TRIGGER reject_canary BEFORE INSERT ON audit_events WHEN NEW.event = 'operation_accepted' BEGIN SELECT RAISE(ABORT, 'private-canary-value'); END"
        )

        assert {:error, :persistence_failed} =
                 F.accept(c.owner, "other", DB.request(c.host), "cli")
      end)

    refute log =~ "private-canary-value"
    refute log =~ "private owner"
    audit = DB.sql("SELECT * FROM audit_events").rows |> inspect()
    refute audit =~ "private-canary-value"
    refute audit =~ "private owner"
  end

  test "candidate rejects malformed effective config without any result mutation", c do
    {:ok, {:accepted, operation}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    {:ok, {:claimed, _}} = F.claim(c.owner, c.host, operation.id, 1)
    candidate = F.bindings(c.owner, c.host, operation.id)["candidate"]

    invalid = [
      false,
      true,
      0,
      "config",
      [],
      %{},
      %{"unexpected" => "private-canary"},
      Map.delete(DB.config(), "commit"),
      %{DB.config() | "submodules" => [%{} | :bad_tail]}
    ]

    for config <- invalid do
      observation = %{
        "runtime_id" => candidate["id"],
        "generation" => 1,
        "observed_state" => "running",
        "effective_config" => config
      }

      assert {:error, :invalid_result} =
               F.finish(
                 c.owner,
                 c.host,
                 operation.id,
                 1,
                 "executing",
                 F.bindings(c.owner, c.host, operation.id),
                 %{"status" => "succeeded", "code" => "ok"},
                 observation
               )

      assert DB.sql("SELECT observed_state, effective_config FROM runtime_instances").rows == [
               ["pending", nil]
             ]

      assert DB.sql("SELECT status, terminal_result FROM operations").rows == [["executing", nil]]

      assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'operation_terminal'").rows ==
               [[0]]
    end

    observation = %{
      "runtime_id" => candidate["id"],
      "generation" => 1,
      "observed_state" => "running"
    }

    for {field, malformed} <- [{"provider_instance_id", false}, {"deleted", nil}] do
      assert {:error, :invalid_result} =
               F.finish(
                 c.owner,
                 c.host,
                 operation.id,
                 1,
                 "executing",
                 F.bindings(c.owner, c.host, operation.id),
                 %{"status" => "succeeded", "code" => "ok"},
                 Map.put(observation, field, malformed)
               )
    end

    assert DB.sql("SELECT observed_state, effective_config FROM runtime_instances").rows == [
             ["pending", nil]
           ]

    assert DB.sql("SELECT status, terminal_result FROM operations").rows == [["executing", nil]]

    assert DB.sql("SELECT count(*) FROM audit_events WHERE event = 'operation_terminal'").rows ==
             [[0]]

    normalized = Map.put(observation, "effective_config", DB.config())
    defaulted = Map.put(observation, "effective_config", Map.delete(DB.config(), "submodules"))

    assert {:ok, {:finished, finished}} =
             F.finish(
               c.owner,
               c.host,
               operation.id,
               1,
               "executing",
               F.bindings(c.owner, c.host, operation.id),
               %{"status" => "succeeded", "code" => "ok"},
               defaulted
             )

    assert finished.terminal_result["observation"]["effective_config"] == DB.config()

    assert {:ok, {:replayed, _}} =
             F.finish(
               c.owner,
               c.host,
               operation.id,
               1,
               "executing",
               F.bindings(c.owner, c.host, operation.id),
               %{"status" => "succeeded", "code" => "ok"},
               normalized
             )
  end

  test "target rejects malformed effective config and retains known revisions", c do
    {:ok, {:accepted, create}} = F.accept(c.owner, "create", DB.request(c.host), "cli")
    target = DB.current!(create)
    DB.finish!(c.owner, c.host, create)

    {:ok, {:accepted, operation}} =
      F.accept(c.owner, "stop", DB.request(c.host, create.workspace_id, "stop"), "cli")

    {:ok, {:claimed, _}} = F.claim(c.owner, c.host, operation.id, 2)

    for config <- [false, 0, [], "config", %{}, %{"unexpected" => "private-canary"}] do
      observation = %{
        "runtime_id" => target["id"],
        "generation" => 1,
        "observed_state" => "stopped",
        "effective_config" => config
      }

      assert {:error, :invalid_result} =
               F.finish(
                 c.owner,
                 c.host,
                 operation.id,
                 2,
                 "executing",
                 F.bindings(c.owner, c.host, operation.id),
                 %{"status" => "succeeded", "code" => "ok"},
                 observation
               )

      assert DB.sql("SELECT observed_state, effective_config FROM runtime_instances").rows == [
               ["running", Jason.encode!(DB.config())]
             ]

      assert DB.sql("SELECT status, terminal_result FROM operations WHERE id = ?", [operation.id]).rows ==
               [["executing", nil]]

      assert DB.sql(
               "SELECT count(*) FROM audit_events WHERE operation_id = ? AND event = 'operation_terminal'",
               [operation.id]
             ).rows == [[0]]
    end
  end

  defp clone_operation!(source, options \\ []) do
    id = Ecto.UUID.generate()
    status = Keyword.get(options, :status, "failed")

    terminal =
      if status == "failed",
        do:
          Jason.encode!(%{
            "outcome" => %{"status" => "failed", "code" => "operation_failed"},
            "observation" => nil
          }),
        else: nil

    DB.sql(
      "INSERT INTO operations SELECT ?, ?, ?, ?, action, request_version, request_bytes, request_hash, ?, ?, execution_config_snapshot, ?, inserted_at, updated_at FROM operations WHERE id = ?",
      [
        id,
        Keyword.get(options, :owner, source.owner_id),
        Keyword.get(options, :workspace, source.workspace_id),
        Keyword.get(options, :key, Ecto.UUID.generate()),
        Keyword.get(options, :version, source.intent_version + 1),
        status,
        terminal,
        source.id
      ]
    )

    id
  end

  defp race(repo, action) do
    parent = self()

    tasks =
      for index <- 1..2 do
        Task.async(fn ->
          Repo.put_dynamic_repo(repo)
          send(parent, :ready)

          receive do
            :go -> action.(index)
          end
        end)
      end

    for _ <- tasks, do: assert_receive(:ready, 5000)
    for task <- tasks, do: send(task.pid, :go)
    Enum.map(tasks, &Task.await(&1, 10_000))
  end

  defp wait_ready(port, output, deadline \\ System.monotonic_time(:millisecond) + 20_000) do
    remaining = max(deadline - System.monotonic_time(:millisecond), 0)

    receive do
      {^port, {:data, bytes}} ->
        combined = output <> bytes
        if byte_size(combined) > 65_536, do: flunk("child output exceeded test bound")

        if "T005_READY" in String.split(combined, "\n"),
          do: :ok,
          else: wait_ready(port, combined, deadline)

      {^port, {:exit_status, status}} ->
        flunk("child exited #{status}: #{output}")
    after
      remaining -> flunk("child never reached crash boundary: #{output}")
    end
  end
end
