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

  test "first heartbeat after hello passes and a catch-up burst within capacity stays connected",
       c do
    ref = connect(c)
    Agent.update(c.mono, fn _ -> 10_000 end)

    for _ <- 1..3 do
      assert {:ok, %{"kind" => "heartbeat_ack"}} =
               HostConnections.heartbeat(ref, message(c, "heartbeat"))
    end

    refute_received {:host_connection_close, _}
  end

  defp audits(c) do
    Repo.query!(
      "SELECT event FROM audit_events WHERE host_id=? AND (event LIKE 'host_conn%' OR event LIKE 'host_heartbeat%') ORDER BY rowid",
      [c.peer.host_id]
    ).rows
    |> List.flatten()
  end

  defp authenticated_audits(c) do
    Repo.query!(
      "SELECT owner_id,host_id,action,outcome,channel FROM audit_events WHERE host_id=? AND event='host_connection_authenticated' ORDER BY rowid",
      [c.peer.host_id]
    ).rows
  end

  test "socket open and hello audit no authentication until the first accepted heartbeat", c do
    assert {:ok, ref} = HostConnections.open(c.peer, self())
    assert authenticated_audits(c) == []
    assert {:ok, "unknown"} = HostConnections.host_state(c.peer.host_id)

    assert {:ok, %{"kind" => "hello_ack"}} =
             HostConnections.receive_hello(ref, message(c, "hello"))

    assert authenticated_audits(c) == []
    assert {:ok, "unknown"} = HostConnections.host_state(c.peer.host_id)

    assert {:ok, %{"kind" => "heartbeat_ack"}} =
             HostConnections.heartbeat(ref, message(c, "heartbeat"))

    expected = [[c.peer.owner_id, c.peer.host_id, "host_authenticate", "connected", "internal"]]
    assert authenticated_audits(c) == expected

    for _ <- 1..3 do
      assert {:ok, %{"kind" => "heartbeat_ack"}} =
               HostConnections.heartbeat(ref, message(c, "heartbeat"))
    end

    assert authenticated_audits(c) == expected

    Hosts.sql("UPDATE host_connection_slots SET state='unknown' WHERE host_id=?", [c.peer.host_id])

    for i <- 1..5 do
      Agent.update(c.mono, fn _ -> i * 10_000 end)

      assert {:ok, %{"kind" => "heartbeat_ack"}} =
               HostConnections.heartbeat(ref, message(c, "heartbeat"))
    end

    assert authenticated_audits(c) == expected
  end

  test "replacement authenticates once and the old connection cannot authenticate again", c do
    old = connect(c)
    assert length(authenticated_audits(c)) == 1
    assert {:ok, next} = HostConnections.open(c.peer, self())
    assert_receive {:host_connection_close, :replaced}
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(old, message(c, "heartbeat"))
    assert length(authenticated_audits(c)) == 1

    assert {:ok, %{"kind" => "hello_ack"}} =
             HostConnections.receive_hello(next, message(c, "hello"))

    assert length(authenticated_audits(c)) == 1

    assert {:ok, %{"kind" => "heartbeat_ack"}} =
             HostConnections.heartbeat(next, message(c, "heartbeat"))

    assert length(authenticated_audits(c)) == 2
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(old, message(c, "heartbeat"))
    assert {:ok, _} = HostConnections.heartbeat(next, message(c, "heartbeat"))
    assert length(authenticated_audits(c)) == 2
  end

  test "unnegotiated malformed and foreign first heartbeats never audit authentication", c do
    assert {:ok, ref} = HostConnections.open(c.peer, self())

    assert {:error, :invalid_connection_message} =
             HostConnections.heartbeat(ref, message(c, "heartbeat"))

    assert authenticated_audits(c) == []

    for bad <- [
          Map.put(message(c, "heartbeat"), "schema_version", 2),
          Map.put(message(c, "heartbeat"), "host_id", Ecto.UUID.generate()),
          Map.put(message(c, "heartbeat"), "owner_id", Ecto.UUID.generate()),
          Map.put(message(c, "heartbeat"), "token", "SECRET_SENTINEL")
        ] do
      assert {:ok, next} = HostConnections.open(c.peer, self())
      assert {:ok, _} = HostConnections.receive_hello(next, message(c, "hello"))
      assert {:error, :invalid_connection_message} = HostConnections.heartbeat(next, bad)
      assert authenticated_audits(c) == []
    end
  end

  test "first heartbeat at the stale boundary never audits authentication", c do
    assert {:ok, ref} = HostConnections.open(c.peer, self())
    assert {:ok, _} = HostConnections.receive_hello(ref, message(c, "hello"))
    Agent.update(c.mono, fn _ -> 30000 end)
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert_receive {:host_connection_close, :stale}
    assert authenticated_audits(c) == []
  end

  test "expired identity cannot authenticate its first heartbeat", c do
    assert {:ok, ref} = HostConnections.open(c.peer, self())
    assert {:ok, _} = HostConnections.receive_hello(ref, message(c, "hello"))
    Application.put_env(:workharbor, :clock, fn -> c.peer.expires_at end)
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert authenticated_audits(c) == []
  end

  test "revoked identity cannot authenticate its first heartbeat", c do
    assert {:ok, ref} = HostConnections.open(c.peer, self())
    assert {:ok, _} = HostConnections.receive_hello(ref, message(c, "hello"))
    assert :ok = Hosts.revoke(c.principal, c.peer.host_id)
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert authenticated_audits(c) == []
  end

  test "rate refusal adds no authentication event after the first accepted heartbeat", c do
    ref = connect(c)
    assert length(authenticated_audits(c)) == 1

    for _ <- 1..3 do
      assert {:ok, _} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    end

    assert {:error, :heartbeat_too_fast} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert_receive {:host_connection_close, :heartbeat_too_fast}
    assert length(authenticated_audits(c)) == 1
  end

  test "first authentication audit failure rolls back liveness and sends no heartbeat ack", c do
    assert {:ok, ref} = HostConnections.open(c.peer, self())
    assert {:ok, _} = HostConnections.receive_hello(ref, message(c, "hello"))

    Hosts.sql(
      "UPDATE hosts SET last_seen='2000-01-01T00:00:00Z',lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?",
      [c.peer.host_id]
    )

    before =
      Hosts.one("SELECT last_seen,lease_expires_at FROM hosts WHERE id=?", [c.peer.host_id])

    Hosts.sql(
      "CREATE TRIGGER reject_authentication_audit BEFORE INSERT ON audit_events WHEN NEW.event='host_connection_authenticated' BEGIN SELECT RAISE(ABORT,'SECRET_SENTINEL'); END"
    )

    assert {:error, :persistence_failed} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert_receive {:host_connection_close, :persistence_failed}
    assert authenticated_audits(c) == []

    assert Hosts.one("SELECT last_seen,lease_expires_at FROM hosts WHERE id=?", [c.peer.host_id]) ==
             before

    assert %{"state" => "unknown", "connection_id" => nil} =
             Hosts.one("SELECT state,connection_id FROM host_connection_slots WHERE host_id=?", [
               c.peer.host_id
             ])

    assert {:ok, "unknown"} = HostConnections.host_state(c.peer.host_id)
    refute Map.has_key?(:sys.get_state(c.server).peers, elem(ref, 1))
    assert {:error, :unauthorized_host} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert authenticated_audits(c) == []
  end

  test "sustained heartbeat burst beyond capacity closes with one refusal audit", c do
    ref = connect(c)

    for _ <- 1..3 do
      assert {:ok, _} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    end

    before = audits(c)
    assert {:error, :heartbeat_too_fast} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert_receive {:host_connection_close, :heartbeat_too_fast}
    assert audits(c) -- before == ["host_heartbeat_refused"]
    refute "host_heartbeat" in audits(c)
  end

  test "steady heartbeats add no audit rows and still renew host last_seen and lease", c do
    ref = connect(c)
    before = audits(c)
    assert before == ["host_connected"]

    hb = fn ->
      Hosts.one("SELECT last_seen,lease_expires_at FROM hosts WHERE id=?", [c.peer.host_id])
    end

    Hosts.sql(
      "UPDATE hosts SET last_seen='2000-01-01T00:00:00Z',lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?",
      [c.peer.host_id]
    )

    Hosts.sql("UPDATE host_connection_slots SET state='unknown' WHERE host_id=?", [c.peer.host_id])

    stale = hb.()

    for i <- 1..5 do
      Agent.update(c.mono, fn _ -> i * 10_000 end)

      assert {:ok, %{"kind" => "heartbeat_ack"}} =
               HostConnections.heartbeat(ref, message(c, "heartbeat"))
    end

    assert audits(c) == before
    assert {:ok, "connected"} = HostConnections.host_state(c.peer.host_id)

    assert %{"state" => "connected"} =
             Hosts.one("SELECT state FROM host_connection_slots WHERE host_id=?", [c.peer.host_id])

    now = hb.()
    assert now["last_seen"] > stale["last_seen"]
    assert now["lease_expires_at"] > stale["lease_expires_at"]
  end

  test "clean close then reopen or restart audits no replacement or reset", c do
    ref = connect(c)
    assert :ok = HostConnections.disconnect(ref)
    assert {:ok, _} = HostConnections.open(c.peer, self())

    assert audits(c) == ["host_connected", "host_connection_unknown", "host_connected"]

    ref2 = connect(c)
    assert :ok = HostConnections.disconnect(ref2)
    before = audits(c)
    :ok = stop_supervised(HostConnections)
    start_supervised!({HostConnections, repo: c.repo})
    assert audits(c) == before
  end

  test "reopen after a failed close write audits the superseded connection once", c do
    ref = connect(c)

    Hosts.sql(
      "CREATE TRIGGER fail_close BEFORE UPDATE ON host_connection_slots BEGIN SELECT RAISE(ABORT,'denied'); END"
    )

    assert {:error, :persistence_failed} = HostConnections.disconnect(ref)
    Hosts.sql("DROP TRIGGER fail_close")
    before = audits(c)
    assert {:ok, _} = HostConnections.open(c.peer, self())
    assert audits(c) -- before == ["host_connection_replaced", "host_connected"]
  end

  test "each liveness transition adds exactly one audit row", c do
    ref = connect(c)
    assert audits(c) == ["host_connected"]

    assert {:ok, ref2} = HostConnections.open(c.peer, self())
    assert audits(c) == ["host_connected", "host_connection_replaced", "host_connected"]
    assert_receive {:host_connection_close, :replaced}
    assert {:error, _} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert audits(c) == ["host_connected", "host_connection_replaced", "host_connected"]

    assert :ok = HostConnections.disconnect(ref2)

    assert audits(c) == [
             "host_connected",
             "host_connection_replaced",
             "host_connected",
             "host_connection_unknown"
           ]
  end

  test "tokens refill at one per five seconds measured from arrival", c do
    ref = connect(c)

    for _ <- 1..3, do: assert({:ok, _} = HostConnections.heartbeat(ref, message(c, "heartbeat")))

    Agent.update(c.mono, fn _ -> 5000 end)
    assert {:ok, _} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
    assert {:error, :heartbeat_too_fast} = HostConnections.heartbeat(ref, message(c, "heartbeat"))
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

  test "repeated reconnects keep one topic subscription and release it on close", c do
    topic = "host: " <> c.peer.host_id

    subscriptions = fn ->
      Registry.lookup(WorkHarbor.PubSub, topic) |> Enum.count(&(elem(&1, 0) == c.server))
    end

    refs = for _ <- 1..4, do: connect(c)
    assert subscriptions.() == 1
    assert :ok = HostConnections.disconnect(List.last(refs))
    assert subscriptions.() == 0
  end

  test "failed replacement keeps the existing socket open and admitted", c do
    ref = connect(c)

    Hosts.sql(
      "CREATE TRIGGER deny_slot_update BEFORE UPDATE OF connection_id ON host_connection_slots WHEN new.connection_id IS NOT old.connection_id BEGIN SELECT RAISE(ABORT,'denied'); END"
    )

    assert {:error, :unauthorized_host} = HostConnections.open(c.peer, self())
    refute_receive {:host_connection_close, _}, 50

    assert {:ok, %{"kind" => "heartbeat_ack"}} =
             HostConnections.heartbeat(ref, message(c, "heartbeat"))

    assert {:ok, "connected"} = HostConnections.host_state(c.peer.host_id)
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
    Agent.update(c.mono, fn _ -> 35000 end)
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

    Agent.update(c.mono, fn _ -> 5000 end)
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
