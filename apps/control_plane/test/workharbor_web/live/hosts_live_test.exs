# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.HostsLiveTest do
  use ExUnit.Case, async: false
  import Plug.Conn
  import Phoenix.ConnTest
  import Phoenix.LiveViewTest
  alias WorkHarbor.{Accounts, Hosts, Repo}
  alias WorkHarbor.Foundation.{Host, Owner}
  alias WorkHarborWeb.{BrowserSession, Endpoint}
  @endpoint Endpoint

  setup tags do
    Repo.put_dynamic_repo(Repo)
    WorkHarbor.DataCase.setup_sandbox(tags)
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, principal} = Accounts.login(owner, secret, "owner_browser")
    %{principal: principal, conn: signed_in(token, principal), secret: secret}
  end

  test "hosts page visibly escapes hostile metadata and never exposes identity secrets", c do
    host =
      Repo.insert!(%Host{
        owner_id: c.principal.owner_id,
        capabilities: ["<script>CANARY</script>\n\e"],
        identity_fingerprint: "PRIVATE-IDENTITY-CANARY"
      })

    page = get(c.conn, "https://localhost/owner/hosts")
    assert html_response(page, 200) =~ "Hosts"
    {:ok, view, html} = live(page)
    assert html =~ host.id
    assert html =~ "&lt;script&gt;CANARY&lt;/script&gt;&lt;U+000A&gt;&lt;U+001B&gt;"
    refute html =~ "PRIVATE-IDENTITY-CANARY"
    refute has_element?(view, "form")
    refute has_element?(view, "[phx-submit]")
    assert has_element?(view, "nav a[href='/owner/hosts']")
  end

  test "shared host reads bind owner and enforce the three credential kinds", c do
    ours = Repo.insert!(%Host{owner_id: c.principal.owner_id})
    other = Repo.insert!(%Owner{display_name: "foreign"})
    foreign = Repo.insert!(%Host{owner_id: other.id})
    assert {:ok, %{rows: [row]}} = Hosts.list_status(c.principal)
    assert row.id == ours.id
    refute inspect(row) =~ foreign.id
    {:ok, _, cli} = Accounts.login(c.principal.owner_id, c.secret, "cli_owner")
    assert {:ok, %{rows: [_]}} = Hosts.list_status(cli)
    {:ok, _, dashboard} = Accounts.login(c.principal.owner_id, c.secret, "dashboard")
    assert {:error, :forbidden} = Hosts.list_status(dashboard)
  end

  test "host pagination is stable bounded and rejects bad query values before reads", c do
    for _ <- 1..27, do: Repo.insert!(%Host{owner_id: c.principal.owner_id})
    assert {:ok, %{rows: rows, has_next: true}} = Hosts.list_status(c.principal)
    assert length(rows) == 25
    assert Enum.map(rows, & &1.id) == Enum.sort(Enum.map(rows, & &1.id))
    assert {:ok, %{rows: tail, has_next: false}} = Hosts.list_status(c.principal, 2, 25)
    assert length(tail) == 2

    for query <- [
          "page=0",
          "size=51",
          "page=1001",
          "page=1&page=2",
          "token=SECRET",
          "page=-1",
          "size=x",
          "page=1&size=25&extra=1",
          "page=" <> String.duplicate("1", 128)
        ] do
      response = get(c.conn, "https://localhost/owner/hosts?" <> query)
      assert response.status == 400
      assert Jason.decode!(response.resp_body) == %{"error" => %{"code" => "invalid_request"}}
    end

    assert get(c.conn, "https://localhost/owner/hosts?page=2&size=25").status == 200
  end

  test "empty page and unknown connection are explicit", c do
    {:ok, view, html} = live(get(c.conn, "https://localhost/owner/hosts"))
    assert html =~ "No hosts recorded"
    Repo.insert!(%Host{owner_id: c.principal.owner_id})
    send(view.pid, {:owner_session, :sys.get_state(view.pid).socket.private.owner_auth.ref})
    assert render(view) =~ "Unknown"
    refute render(view) =~ "No hosts recorded"
  end

  test "forged events do not change host records audit or logs", c do
    {:ok, view, _} = live(get(c.conn, "https://localhost/owner/hosts"))
    before = Repo.query!("SELECT COUNT(*) FROM audit_events", [], log: false).rows

    log =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        render_hook(view, "delete", %{"payload" => "FORGED-HOST-CANARY"})
        assert Process.alive?(view.pid)
      end)

    refute log =~ "FORGED-HOST-CANARY"
    assert Repo.query!("SELECT COUNT(*) FROM audit_events", [], log: false).rows == before
  end

  test "host list visibly escapes the full FR022 control set", c do
    points =
      (Enum.to_list(0..31) -- [9]) ++
        Enum.to_list(127..159) ++
        [0x061C, 0x200E, 0x200F, 0x2028, 0x2029] ++
        Enum.to_list(0x202A..0x202E) ++ Enum.to_list(0x2066..0x2069)

    values = Enum.map(points, fn point -> "before" <> <<point::utf8>> <> "after" end)
    Repo.insert!(%Host{owner_id: c.principal.owner_id, capabilities: values})
    {:ok, _view, html} = live(get(c.conn, "https://localhost/owner/hosts"))

    for point <- points do
      marker = point |> Integer.to_string(16) |> String.upcase() |> String.pad_leading(4, "0")
      assert html =~ "before&lt;U+#{marker}&gt;after"
      refute html =~ "before" <> <<point::utf8>> <> "after"
    end
  end

  test "host read timeout has one batch budget and displays unavailable", c do
    for _ <- 1..3, do: Repo.insert!(%Host{owner_id: c.principal.owner_id})
    start_supervised!({WorkHarbor.HostConnections, []})
    :sys.suspend(WorkHarbor.HostConnections)

    try do
      started = System.monotonic_time(:millisecond)
      assert {:error, :unavailable} = Hosts.list_status(c.principal)
      assert System.monotonic_time(:millisecond) - started < 2000
      {:ok, _view, html} = live(get(c.conn, "https://localhost/owner/hosts"))
      assert html =~ "Host information is unavailable"
    after
      :sys.resume(WorkHarbor.HostConnections)
    end
  end

  test "revocation first denial clears protected rows and queued ticks never reread", c do
    host = Repo.insert!(%Host{owner_id: c.principal.owner_id})
    {:ok, view, html} = live(get(c.conn, "https://localhost/owner/hosts"))
    assert html =~ host.id
    socket = :sys.get_state(view.pid).socket
    assert :ok = Accounts.revoke(c.principal, c.principal.credential_id)
    before = denied_count()

    assert {:halt, stopped} =
             Phoenix.LiveView.Lifecycle.handle_info(
               {:owner_session, socket.private.owner_auth.ref},
               socket
             )

    assert stopped.private.owner_auth.stopped
    assert stopped.assigns.rows == []
    assert stopped.private.owner_auth.principal == nil
    assert denied_count() == before + 1

    for _ <- 1..3 do
      assert {:halt, ^stopped} =
               Phoenix.LiveView.Lifecycle.handle_info(
                 {:owner_session, socket.private.owner_auth.ref},
                 stopped
               )
    end

    assert denied_count() == before + 1
  end

  test "refresh replaces connected observations with unknown without keeping healthy rows", c do
    Repo.insert!(%Host{owner_id: c.principal.owner_id})
    start_supervised!({__MODULE__.StatusStub, "connected"})
    {:ok, view, html} = live(get(c.conn, "https://localhost/owner/hosts"))
    assert html =~ "Connected"
    GenServer.call(WorkHarbor.HostConnections, {:set, "unknown"})
    socket = :sys.get_state(view.pid).socket
    send(view.pid, {:owner_session, socket.private.owner_auth.ref})
    updated = render(view)
    assert updated =~ "Unknown"
    refute updated =~ ">Connected<"
    assert is_reference(:sys.get_state(view.pid).socket.private.owner_auth.ref)
    refute :sys.get_state(view.pid).socket.private.owner_auth.ref == socket.private.owner_auth.ref
  end

  test "hosts admission refuses missing wrong-kind expired and revoked credentials", c do
    assert_refusal(get(base_conn(), "https://localhost/owner/hosts"), 401, "unauthenticated")

    for kind <- ["dashboard", "cli_owner"] do
      {:ok, token, principal} = Accounts.login(c.principal.owner_id, c.secret, kind)

      assert_refusal(
        get(signed_in(token, principal), "https://localhost/owner/hosts"),
        401,
        "unauthenticated"
      )
    end

    for state <- [:expired, :revoked] do
      {:ok, token, principal} = Accounts.login(c.principal.owner_id, c.secret, "owner_browser")
      conn = signed_in(token, principal)
      old = Application.get_env(:workharbor, :clock)

      try do
        case state do
          :expired -> Application.put_env(:workharbor, :clock, fn -> principal.expires_at end)
          :revoked -> assert :ok = Accounts.revoke(principal, principal.credential_id)
        end

        assert_refusal(get(conn, "https://localhost/owner/hosts"), 401, "unauthenticated")
      after
        restore_env(:clock, old)
      end
    end
  end

  test "hosts navigation rechecks expiry and revocation before reading", c do
    for invalid <- [:expired, :revoked] do
      {:ok, token, principal} = Accounts.login(c.principal.owner_id, c.secret, "owner_browser")
      {:ok, view, _} = live(get(signed_in(token, principal), "https://localhost/owner/hosts"))
      socket = :sys.get_state(view.pid).socket
      GenServer.stop(view.pid, :normal)
      old = Application.get_env(:workharbor, :clock)

      try do
        case invalid do
          :expired -> Application.put_env(:workharbor, :clock, fn -> principal.expires_at end)
          :revoked -> assert :ok = Accounts.revoke(principal, principal.credential_id)
        end

        before = denied_count()

        assert {:halt, stopped} =
                 Phoenix.LiveView.Lifecycle.handle_params(
                   %{},
                   "https://localhost/owner/hosts",
                   socket
                 )

        assert {:redirect, %{to: "/login/owner"}} = stopped.redirected
        assert stopped.private.owner_auth.stopped
        assert stopped.assigns.rows == []
        assert denied_count() == before + 1
      after
        restore_env(:clock, old)
      end
    end
  end

  test "real heartbeat becomes visibly unknown within five seconds including a slow read", c do
    %{mono: mono, peer: peer, server: server} = real_connection(c)
    Agent.update(mono, fn _ -> 29_999 end)
    assert {:ok, "connected"} = WorkHarbor.HostConnections.host_state(peer.host_id)

    for delay <- [false, true] do
      Agent.update(mono, fn _ -> 0 end)
      connect_real(peer)
      Agent.update(mono, fn _ -> 29_999 end)
      {:ok, view, html} = live(get(c.conn, "https://localhost/owner/hosts"))
      assert html =~ "Connected"
      if delay, do: :sys.suspend(server)
      Agent.update(mono, fn _ -> 30_000 end)
      started = System.monotonic_time(:millisecond)

      if delay do
        # Hold the real connection server until the first owner refresh is
        # waiting on it, then hold it through the complete batch deadline.
        try do
          assert_waiting_lookup(server, started + 3000)
          # The actual first timer-driven read must exhaust the whole batch
          # budget while this real connection server remains suspended.
          assert render(view) =~ "Host information is unavailable"
        after
          :sys.resume(server)
        end
      end

      assert_unknown(view, started + 5000)
      assert System.monotonic_time(:millisecond) - started < 5000
      GenServer.stop(view.pid, :normal)
    end
  end

  test "revoked host and recorded timestamps render without a connected conclusion", c do
    stamp = ~U[2026-10-26 12:34:56.000000Z]
    host = Repo.insert!(%Host{owner_id: c.principal.owner_id, last_seen: stamp})

    Repo.query!(
      "INSERT INTO host_security(host_id,owner_id,revoked_at) VALUES(?,?,?)",
      [host.id, c.principal.owner_id, Accounts.now()],
      log: false
    )

    start_supervised!({__MODULE__.StatusStub, "connected"})
    old = Application.get_env(:workharbor, :clock)
    Application.put_env(:workharbor, :clock, fn -> 2_000_000_000 end)
    # The credential must be current in the same injected wall-clock epoch.
    try do
      {:ok, token, principal} = Accounts.login(c.principal.owner_id, c.secret, "owner_browser")
      assert {:ok, %{rows: [row], observed_at: 2_000_000_000}} = Hosts.list_status(principal)
      assert row.revoked
      assert row.connection == "revoked"
      {:ok, _view, html} = live(get(signed_in(token, principal), "https://localhost/owner/hosts"))
      assert html =~ "Revoked"
      refute html =~ ">Connected<"
      assert html =~ "Last seen:"
      assert html =~ "2026-10-26T12:34:56.000000Z"
      assert html =~ "Observed at 2000000000 (Unix seconds)."
    after
      restore_env(:clock, old)
    end
  end

  test "host status denial after page authorization stops once and clears data", c do
    Repo.insert!(%Host{owner_id: c.principal.owner_id})
    {:ok, view, _} = live(get(c.conn, "https://localhost/owner/hosts"))
    socket = :sys.get_state(view.pid).socket
    GenServer.stop(view.pid, :normal)
    assert :ok = Accounts.authorize(c.principal, :owner_page)
    handler = "whr-w2-denial-#{Ecto.UUID.generate()}"
    me = self()
    Process.put(:whr_deny_after_page_read, false)

    :telemetry.attach(
      handler,
      Repo.config()[:telemetry_prefix] ++ [:query],
      fn _, _, metadata, _ ->
        if self() == me and not Process.get(:whr_deny_after_page_read) and
             String.contains?(metadata.query, "FROM \"owner_credentials\"") do
          Process.put(:whr_deny_after_page_read, true)

          Repo.query!(
            "UPDATE owner_credentials SET revoked_at=? WHERE id=?",
            [Accounts.now(), c.principal.credential_id],
            log: false
          )
        end
      end,
      nil
    )

    try do
      before = denied_count()

      assert {:halt, stopped} =
               Phoenix.LiveView.Lifecycle.handle_info(
                 {:owner_session, socket.private.owner_auth.ref},
                 socket
               )

      assert Process.get(:whr_deny_after_page_read)
      assert stopped.private.owner_auth.stopped
      assert stopped.private.owner_auth.principal == nil
      assert stopped.assigns.rows == []
      assert {:redirect, %{to: "/login/owner"}} = stopped.redirected
      assert denied_count() == before + 1

      assert {:halt, ^stopped} =
               Phoenix.LiveView.Lifecycle.handle_info(
                 {:owner_session, socket.private.owner_auth.ref},
                 stopped
               )

      assert denied_count() == before + 1
    after
      :telemetry.detach(handler)
      Process.delete(:whr_deny_after_page_read)
    end
  end

  test "invalid join URL stays rejected across owner timer refresh", c do
    for _ <- 1..3, do: Repo.insert!(%Host{owner_id: c.principal.owner_id})
    {:ok, view, _} = live(get(c.conn, "https://localhost/owner/hosts?page=2&size=1"))
    socket = :sys.get_state(view.pid).socket
    GenServer.stop(view.pid, :normal)

    assert {:noreply, rejected} =
             WorkHarborWeb.HostsLive.handle_params(
               %{},
               "https://localhost/owner/hosts?page=0",
               socket
             )

    assert rejected.assigns.state == :error
    assert rejected.assigns.rows == []

    handler = "whr-w2-rejected-query-#{Ecto.UUID.generate()}"
    me = self()

    :telemetry.attach(
      handler,
      Repo.config()[:telemetry_prefix] ++ [:query],
      fn _, _, metadata, _ ->
        if Regex.match?(~r/\bFROM\s+"?hosts"?\b/i, metadata.query),
          do: send(me, :rejected_host_read)
      end,
      nil
    )

    try do
      assert {:halt, after_tick} =
               Phoenix.LiveView.Lifecycle.handle_info(
                 {:owner_session, rejected.private.owner_auth.ref},
                 rejected
               )

      refute_received :rejected_host_read
      assert after_tick.assigns.state == :error
      assert after_tick.assigns.rows == []
      assert after_tick.assigns.page == 2
      assert after_tick.assigns.size == 1

      for _ <- 1..2 do
        assert {:halt, ^after_tick} =
                 Phoenix.LiveView.Lifecycle.handle_info(
                   {:owner_session, rejected.private.owner_auth.ref},
                   after_tick
                 )
      end

      refute_received :rejected_host_read

      if after_tick.private.owner_auth.timer,
        do: Process.cancel_timer(after_tick.private.owner_auth.timer)
    after
      :telemetry.detach(handler)
    end
  end

  test "pagination links preserve size and suppress next at the page cap", c do
    for _ <- 1..5, do: Repo.insert!(%Host{owner_id: c.principal.owner_id})
    {:ok, view, _} = live(get(c.conn, "https://localhost/owner/hosts?page=2&size=2"))
    assert has_element?(view, "a[href='/owner/hosts?page=1&size=2']", "Previous page")
    assert has_element?(view, "a[href='/owner/hosts?page=3&size=2']", "Next page")
    assigns = %{rows: [], state: :ready, page: 1000, size: 2, has_next: true, observed_at: nil}
    html = render_component(&WorkHarborWeb.HostsLive.render/1, assigns)
    assert html =~ "/owner/hosts?page=999&amp;size=2"
    refute html =~ "Next page"
  end

  test "host pagination rejects trailing controls without raising" do
    assert {:ok, %{page: 1, size: 25}} =
             WorkHarborWeb.Security.owner_list_params("page=00001&size=00025")

    for query <- ["page=1\n", "size=2\n", "page=1\r\n", "page=1%0A", "size=2%00"] do
      assert {:error, :invalid_request} = WorkHarborWeb.Security.owner_list_params(query)
    end
  end

  test "owner navigation names the current page", c do
    for {path, expected} <- [{"/owner", "/owner"}, {"/owner/hosts", "/owner/hosts"}] do
      {:ok, view, _} = live(get(c.conn, "https://localhost" <> path))

      assert has_element?(
               view,
               "nav[aria-label='Owner navigation'] a[href='#{expected}'][aria-current='page']"
             )

      assert length(Regex.scan(~r/aria-current="page"/, render(view))) == 1
      GenServer.stop(view.pid, :normal)
    end
  end

  test "batch timeout is deterministic when the reader wakes after inner expiry", c do
    Repo.insert!(%Host{owner_id: c.principal.owner_id})
    start_supervised!({__MODULE__.HeldLookup, self()})
    repo = Repo.get_dynamic_repo()

    reader =
      Task.async(fn ->
        Repo.put_dynamic_repo(repo)
        Hosts.list_status(c.principal)
      end)

    assert_receive {:lookup_started, worker}, 1000
    true = :erlang.suspend_process(reader.pid)
    Process.send_after(self(), :resume_batch_reader, 1200)

    try do
      assert_receive :resume_batch_reader, 2000
      true = :erlang.resume_process(reader.pid)
      assert {:error, :unavailable} = Task.await(reader, 2000)
      refute Process.alive?(worker)
    after
      if Process.alive?(reader.pid), do: :erlang.resume_process(reader.pid)
      Task.shutdown(reader, :brutal_kill)
    end
  end

  defmodule HeldLookup do
    use GenServer

    def start_link(observer),
      do: GenServer.start_link(__MODULE__, observer, name: WorkHarbor.HostConnections)

    def init(observer), do: {:ok, observer}

    def handle_call({:host_state, _}, {worker, _}, observer) do
      send(observer, {:lookup_started, worker})
      {:noreply, observer}
    end
  end

  defp assert_refusal(conn, status, reason) do
    assert conn.status == status
    assert Jason.decode!(conn.resp_body) == %{"error" => %{"code" => reason}}
  end

  defp restore_env(key, nil), do: Application.delete_env(:workharbor, key)
  defp restore_env(key, value), do: Application.put_env(:workharbor, key, value)

  defp real_connection(c) do
    dir =
      Path.join(WorkHarbor.HostFixture.physical_temp(), "whr-w2-issuer-#{Ecto.UUID.generate()}")

    old = Application.get_env(:workharbor, :host_identity)
    old_mono = Application.get_env(:workharbor, :host_monotonic_clock)
    assert {:ok, _} = WorkHarbor.Hosts.Issuer.initialize(dir)
    Application.put_env(:workharbor, :host_identity, issuer_dir: dir)
    {csr, _} = WorkHarbor.HostFixture.csr()
    assert {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    assert {:ok, enrolled} = Hosts.redeem(issued.code, csr)
    assert {:ok, peer} = Hosts.resolve_identity(Base.decode64!(enrolled["certificate"]), :active)
    mono = start_supervised!({Agent, fn -> 0 end})
    Application.put_env(:workharbor, :host_monotonic_clock, fn -> Agent.get(mono, & &1) end)
    server = start_supervised!({WorkHarbor.HostConnections, []})
    assert {:ok, ref} = WorkHarbor.HostConnections.open(peer, self())

    for kind <- ["hello", "heartbeat"] do
      msg = %{
        "schema_version" => 1,
        "kind" => kind,
        "owner_id" => peer.owner_id,
        "host_id" => peer.host_id
      }

      msg = if kind == "hello", do: Map.put(msg, "capabilities", []), else: msg

      assert {:ok, _} =
               if(kind == "hello",
                 do: WorkHarbor.HostConnections.receive_hello(ref, msg),
                 else: WorkHarbor.HostConnections.heartbeat(ref, msg)
               )
    end

    on_exit(fn ->
      restore_env(:host_identity, old)
      restore_env(:host_monotonic_clock, old_mono)
      File.rm_rf!(dir)
    end)

    %{mono: mono, peer: peer, server: server}
  end

  defp connect_real(peer) do
    assert {:ok, ref} = WorkHarbor.HostConnections.open(peer, self())

    for kind <- ["hello", "heartbeat"] do
      msg = %{
        "schema_version" => 1,
        "kind" => kind,
        "owner_id" => peer.owner_id,
        "host_id" => peer.host_id
      }

      msg = if kind == "hello", do: Map.put(msg, "capabilities", []), else: msg

      assert {:ok, _} =
               if(kind == "hello",
                 do: WorkHarbor.HostConnections.receive_hello(ref, msg),
                 else: WorkHarbor.HostConnections.heartbeat(ref, msg)
               )
    end

    ref
  end

  defp assert_unknown(view, deadline) do
    html = render(view)

    if html =~ ">Connected<" or not (html =~ "Unknown") do
      assert System.monotonic_time(:millisecond) < deadline,
             "stale host must display Unknown within five seconds"

      receive do
      after
        10 -> assert_unknown(view, deadline)
      end
    end
  end

  defp assert_waiting_lookup(server, deadline) do
    {:messages, messages} = Process.info(server, :messages)

    if not Enum.any?(messages, &match?({:"$gen_call", _, {:host_state, _}}, &1)) do
      assert System.monotonic_time(:millisecond) < deadline, "refresh must start a host lookup"

      receive do
      after
        10 -> assert_waiting_lookup(server, deadline)
      end
    end
  end

  defmodule StatusStub do
    use GenServer

    def start_link(status),
      do: GenServer.start_link(__MODULE__, status, name: WorkHarbor.HostConnections)

    def init(status), do: {:ok, status}
    def handle_call({:host_state, _}, _, status), do: {:reply, {:ok, status}, status}
    def handle_call({:set, status}, _, _), do: {:reply, :ok, status}
  end

  defp denied_count do
    [[n]] =
      Repo.query!("SELECT COUNT(*) FROM audit_events WHERE event='authorization_denied'", [],
        log: false
      ).rows

    n
  end

  defp signed_in(token, principal) do
    response =
      build_conn()
      |> Map.put(:secret_key_base, Endpoint.config(:secret_key_base))
      |> BrowserSession.install(:owner, token, principal.expires_at)

    cookie = response.resp_cookies["__Host-workharbor_owner"].value
    conn = base_conn()
    put_req_header(conn, "cookie", "__Host-workharbor_owner=" <> cookie)
  end

  defp base_conn do
    conn = build_conn()

    %{
      conn
      | scheme: :https,
        host: "localhost",
        port: 443,
        req_headers: [{"host", "localhost"}]
    }
  end
end
