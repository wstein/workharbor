# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostConnectionsTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Accounts, Hosts, HostFixture, HostConnections, Repo}
  alias WorkHarbor.Hosts.Issuer

  setup do
    dir = Path.join(HostFixture.physical_temp(), "whr-host-test-#{Ecto.UUID.generate()}")
    File.mkdir!(dir)
    File.chmod!(dir, 0o700)
    previous = Application.get_env(:workharbor, :host_identity)
    clock = Application.get_env(:workharbor, :clock)
    pid = WorkHarbor.HostDatabase.open!(Path.join(dir, "state.db"))
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, _} = Accounts.login(owner, secret, "cli_owner")
    {:ok, principal} = Accounts.authenticate(token)
    issuer_dir = Path.join(dir, "issuer")

    on_exit(fn ->
      if previous,
        do: Application.put_env(:workharbor, :host_identity, previous),
        else: Application.delete_env(:workharbor, :host_identity)

      if clock,
        do: Application.put_env(:workharbor, :clock, clock),
        else: Application.delete_env(:workharbor, :clock)

      if Process.alive?(pid), do: GenServer.stop(pid)
      File.rm_rf!(dir)
    end)

    assert {:ok, _} = Issuer.initialize(issuer_dir)
    Application.put_env(:workharbor, :host_identity, issuer_dir: issuer_dir)
    {csr, _} = HostFixture.csr()
    {:ok, issued} = Hosts.issue_enrollment(principal, nil)
    {:ok, enrolled} = Hosts.redeem(issued.code, csr)
    {:ok, peer} = Hosts.resolve_identity(Base.decode64!(enrolled["certificate"]), :active)
    {:ok, mono} = Agent.start_link(fn -> 0 end)
    previous_mono = Application.get_env(:workharbor, :host_monotonic_clock)
    Application.put_env(:workharbor, :host_monotonic_clock, fn -> Agent.get(mono, & &1) end)

    on_exit(fn ->
      if previous_mono,
        do: Application.put_env(:workharbor, :host_monotonic_clock, previous_mono),
        else: Application.delete_env(:workharbor, :host_monotonic_clock)
    end)

    server = start_supervised!({HostConnections, repo: pid})

    %{
      dir: dir,
      issuer_dir: issuer_dir,
      principal: principal,
      repo: pid,
      peer: peer,
      mono: mono,
      server: server
    }
  end

  defp message(c, kind) do
    m = %{
      "schema_version" => 1,
      "kind" => kind,
      "owner_id" => c.peer.owner_id,
      "host_id" => c.peer.host_id
    }

    if kind == "hello", do: Map.put(m, "capabilities", []), else: m
  end

  defp connect(c) do
    {:ok, ref} = HostConnections.open(c.peer, self())

    assert {:ok, %{"kind" => "hello_ack"}} =
             HostConnections.receive_hello(ref, message(c, "hello"))

    assert {:ok, %{"kind" => "heartbeat_ack"}} =
             HostConnections.heartbeat(ref, message(c, "heartbeat"))

    ref
  end

  defp runtime(c) do
    {:ok, {:accepted, operation}} =
      WorkHarbor.Foundation.accept(
        c.peer.owner_id,
        "connection-fixture",
        WorkHarbor.FoundationDatabase.request(c.peer.host_id),
        "internal"
      )

    binding = WorkHarbor.FoundationDatabase.current!(operation)
    {operation, binding}
  end

  test "authenticated heartbeat becomes stale exactly30seconds without changing history", c do
    ref = connect(c)
    {operation, binding} = runtime(c)

    assert :ok =
             HostConnections.record_fresh_observation(
               ref,
               binding["id"],
               binding["generation"],
               operation.intent_version
             )

    assert {:ok, %{observed_state: "running"}} =
             HostConnections.project_runtime(c.principal, binding["id"])

    Agent.update(c.mono, fn _ -> 29999 end)

    assert {:ok, %{observed_state: "running"}} =
             HostConnections.project_runtime(c.principal, binding["id"])

    Agent.update(c.mono, fn _ -> 30000 end)
    send(c.server, :tick)

    assert {:ok, %{observed_state: "unknown"}} =
             HostConnections.project_runtime(c.principal, binding["id"])

    assert_receive {:host_connection_close, :stale}

    assert Repo.get!(WorkHarbor.Foundation.RuntimeInstance, binding["id"]).observed_state ==
             "running"

    assert Repo.get!(WorkHarbor.Foundation.Operation, operation.id).status == "queued"
  end

  test "reconnect and replaced socket cannot refresh old runtime annotation", c do
    ref = connect(c)
    {operation, binding} = runtime(c)

    assert :ok =
             HostConnections.record_fresh_observation(
               ref,
               binding["id"],
               binding["generation"],
               operation.intent_version
             )

    next = connect(c)
    assert_receive {:host_connection_close, :replaced}
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))

    assert {:ok, %{observed_state: "unknown"}} =
             HostConnections.project_runtime(c.principal, binding["id"])

    assert :ok =
             HostConnections.record_fresh_observation(
               next,
               binding["id"],
               binding["generation"],
               operation.intent_version
             )

    assert {:ok, %{observed_state: "running"}} =
             HostConnections.project_runtime(c.principal, binding["id"])

    assert {:error, :stale_observation} =
             HostConnections.record_fresh_observation(
               next,
               binding["id"],
               binding["generation"] + 1,
               operation.intent_version
             )

    assert {:error, :stale_observation} =
             HostConnections.record_fresh_observation(
               next,
               binding["id"],
               binding["generation"],
               operation.intent_version + 1
             )
  end

  test "revocation and open socket certificate expiry fence existing connections", c do
    ref = connect(c)
    assert :ok = Hosts.revoke(c.principal, c.peer.host_id)
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert {:error, :unauthorized_host} = HostConnections.open(c.peer, self())
    assert_receive {:host_connection_close, _}
  end

  test "closed connection messages reject malformed scope fields versions and bounds", c do
    valid = message(c, "heartbeat")

    for bad <- [
          Map.put(valid, "schema_version", 2),
          Map.put(valid, "schema_version", 1.0),
          Map.put(valid, "host_id", c.peer.owner_id),
          Map.put(valid, "token", "SECRET_SENTINEL")
        ] do
      assert {:error, _} =
               WorkHarbor.HostConnections.Protocol.decode(
                 Jason.encode!(bad),
                 c.peer.owner_id,
                 c.peer.host_id
               )
    end

    assert {:error, _} =
             WorkHarbor.HostConnections.Protocol.decode(
               Jason.encode!(valid) <> String.duplicate(" ", 4096),
               c.peer.owner_id,
               c.peer.host_id
             )

    assert {:error, _} =
             WorkHarbor.HostConnections.Protocol.decode(
               ~s({"schema_version":1,"schema_version":1}),
               c.peer.owner_id,
               c.peer.host_id
             )
  end

  test "late heartbeat at stale boundary cannot revive an existing socket", c do
    ref = connect(c)
    Agent.update(c.mono, fn _ -> 30000 end)
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert_receive {:host_connection_close, :stale}
  end

  test "renewed lease and replaced connection fence delayed stale timers", c do
    ref = connect(c)
    old_lease = :sys.get_state(c.server).peers[elem(ref, 1)].lease
    Agent.update(c.mono, fn _ -> 29000 end)
    {:ok, _} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    Agent.update(c.mono, fn _ -> 30000 end)
    send(c.server, {:stale, elem(ref, 1), old_lease})
    assert {:ok, "connected"} = HostConnections.host_state(c.peer.host_id)
    next = connect(c)
    send(c.server, {:stale, elem(ref, 1), old_lease})
    assert {:ok, "connected"} = HostConnections.host_state(c.peer.host_id)
    assert {:ok, _} = HostConnections.heartbeat(next, message(c, "heartbeat"))
  end

  test "restart invalidates known observations without changing retained operation history", c do
    ref = connect(c)
    {operation, binding} = runtime(c)

    :ok =
      HostConnections.record_fresh_observation(
        ref,
        binding["id"],
        binding["generation"],
        operation.intent_version
      )

    :ok = stop_supervised(HostConnections)
    start_supervised!({HostConnections, repo: c.repo})

    assert {:ok, %{observed_state: "unknown"}} =
             HostConnections.project_runtime(c.principal, binding["id"])

    assert Repo.get!(WorkHarbor.Foundation.RuntimeInstance, binding["id"]).observed_state ==
             "running"

    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
  end

  test "open socket absolute certificate expiry rejects further traffic", c do
    ref = connect(c)
    Application.put_env(:workharbor, :clock, fn -> c.peer.expires_at end)
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert_receive {:host_connection_close, :identity_invalid}
    assert {:error, :unauthorized_host} = HostConnections.open(c.peer, self())
  end

  test "failed persistence leaves conservative unknown and fixed failure", c do
    ref = connect(c)
    {operation, binding} = runtime(c)

    :ok =
      HostConnections.record_fresh_observation(
        ref,
        binding["id"],
        binding["generation"],
        operation.intent_version
      )

    Hosts.sql(
      "CREATE TRIGGER fail_liveness BEFORE UPDATE ON host_connection_slots BEGIN SELECT RAISE(ABORT,'SECRET_SENTINEL'); END"
    )

    assert {:error, :persistence_failed} = HostConnections.heartbeat(ref, message(c, "heartbeat"))

    assert {:ok, %{observed_state: "unknown"}} =
             HostConnections.project_runtime(c.principal, binding["id"])
  end

  defp await_projection(pid, remaining) do
    found =
      Enum.any?(elem(Process.info(pid, :messages), 1), fn
        {:"$gen_call", _, message} when is_tuple(message) -> elem(message, 0) == :project
        _ -> false
      end)

    cond do
      found ->
        :ok

      remaining == 0 ->
        flunk("projection did not reach worker")

      true ->
        Process.sleep(1)
        await_projection(pid, remaining - 1)
    end
  end

  test "projection rechecks authoritative current intent inside worker", c do
    ref = connect(c)
    {operation, binding} = runtime(c)

    :ok =
      HostConnections.record_fresh_observation(
        ref,
        binding["id"],
        binding["generation"],
        operation.intent_version
      )

    :sys.suspend(c.server)

    task =
      Task.async(fn ->
        Repo.put_dynamic_repo(c.repo)
        HostConnections.project_runtime(c.principal, binding["id"])
      end)

    try do
      await_projection(c.server, 1000)
      WorkHarbor.FoundationDatabase.finish!(c.peer.owner_id, c.peer.host_id, operation)

      request =
        WorkHarbor.FoundationDatabase.request(c.peer.host_id, operation.workspace_id, "stop")

      assert {:ok, {:accepted, _}} =
               WorkHarbor.Foundation.accept(c.peer.owner_id, "newer-intent", request, "internal")
    after
      :sys.resume(c.server)
    end

    assert {:ok, %{observed_state: "unknown"}} = Task.await(task)
  end

  test "stale audit rollback reports fixed persistence failure", c do
    ref = connect(c)
    entry = :sys.get_state(c.server).peers[elem(ref, 1)]

    Hosts.sql(
      "CREATE TRIGGER reject_stale_audit BEFORE INSERT ON audit_events WHEN NEW.event='host_connection_unknown' BEGIN SELECT RAISE(ABORT,'SECRET_SENTINEL'); END"
    )

    Agent.update(c.mono, fn _ -> 30000 end)
    assert {:error, :persistence_failed} = HostConnections.mark_stale(ref, entry.lease)
  end

  test "dedicated monotonic certificate expiry timer fences idle and replaced sockets", c do
    ref = connect(c)
    entry = :sys.get_state(c.server).peers[elem(ref, 1)]
    assert is_reference(Map.get(entry, :expiry_timer))
    assert is_integer(Process.read_timer(entry.expiry_timer))
    send(c.server, {:expire, elem(ref, 1), entry.id})
    assert {:ok, "connected"} = HostConnections.host_state(c.peer.host_id)
    next = connect(c)
    assert_receive {:host_connection_close, :replaced}
    assert Process.read_timer(entry.expiry_timer) == false
    send(c.server, {:expire, elem(ref, 1), entry.id})
    assert {:ok, "connected"} = HostConnections.host_state(c.peer.host_id)
    next_entry = :sys.get_state(c.server).peers[elem(next, 1)]
    Agent.update(c.mono, fn _ -> next_entry.expiry end)
    send(c.server, {:expire, elem(next, 1), next_entry.id})
    assert_receive {:host_connection_close, :identity_invalid}
    assert {:ok, "unknown"} = HostConnections.host_state(c.peer.host_id)
  end

  test "agent socket lifetime is fenced by owning connection worker death", c do
    assert {:ok, state} = WorkHarborWeb.AgentSocket.init(c.peer)
    assert is_reference(Map.get(state, :worker_monitor))
    monitor = state.worker_monitor
    assert :ok = stop_supervised(HostConnections)
    assert_receive {:DOWN, ^monitor, :process, _, _} = down

    assert {:stop, :normal, 1008, ^state} =
             WorkHarborWeb.AgentSocket.handle_info(down, state)
  end

  test "shared closed host connection fixtures match native validator" do
    path =
      Path.expand("../../specs/006-whr-cli/contracts/host-connection.fixtures.json", File.cwd!())

    fixture = path |> File.read!() |> Jason.decode!()

    for c <- fixture["cases"] do
      result =
        WorkHarbor.HostConnections.Protocol.decode(
          c["raw"],
          fixture["owner_id"],
          fixture["host_id"]
        )

      assert match?({:ok, _}, result) == c["accepted"], c["name"]
    end
  end

  test "rejected application data closes normally without crashing or echoing input", c do
    assert {:ok, state} = WorkHarborWeb.AgentSocket.init(c.peer)

    raw =
      Jason.encode!(%{
        schema_version: 1,
        kind: "hello",
        owner_id: c.peer.owner_id,
        host_id: c.peer.host_id,
        capabilities: [],
        token: "SECRET_SENTINEL"
      })

    assert {:stop, :normal, 1008, ^state} =
             WorkHarborWeb.AgentSocket.handle_in({raw, [opcode: :text]}, state)
  end
end
