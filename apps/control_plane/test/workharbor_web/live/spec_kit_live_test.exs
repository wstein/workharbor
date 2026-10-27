# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.SpecKitLiveTest do
  use ExUnit.Case, async: false
  import Ecto.Query
  import Plug.Conn
  import Phoenix.ConnTest
  import Phoenix.LiveViewTest
  alias WorkHarbor.{Accounts, ConfigTrust, Repo}
  alias WorkHarbor.Foundation.{Host, Workspace}
  alias WorkHarbor.FoundationDatabase, as: DB
  alias WorkHarborWeb.{BrowserSession, Endpoint}
  @endpoint Endpoint

  setup tags do
    Repo.put_dynamic_repo(Repo)
    WorkHarbor.DataCase.setup_sandbox(tags)
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, _, cli} = Accounts.login(owner, secret, "cli_owner")
    {:ok, token, dashboard} = Accounts.login(owner, secret, "dashboard")
    host = Repo.insert!(%Host{owner_id: owner})

    workspace =
      Repo.insert!(%Workspace{
        owner_id: owner,
        host_id: host.id,
        desired_state: "stopped",
        requested_config: DB.config()
      })

    %{
      conn: signed_in(token, dashboard),
      dashboard: dashboard,
      token: token,
      cli: cli,
      workspace: workspace,
      secret: secret
    }
  end

  test "dashboard renders explicit unavailable provenance without artifacts or inferred status",
       c do
    {:ok, _} = ConfigTrust.approve(c.cli, DB.config())
    page = get(c.conn, url(c.workspace.id))
    assert page.status == 200
    assert page.resp_body =~ " · Spec Kit"

    assert get_resp_header(page, "content-security-policy") == [
             "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self' wss://localhost; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
           ]

    {:ok, view, html} = live(page)
    assert html =~ "Artifacts unavailable"
    assert html =~ "Artifact reads from the workspace host are not yet available"
    assert html =~ DB.config()["commit"]
    assert html =~ c.workspace.id
    assert html =~ "Artifact layout and content provenance are unknown"
    assert html =~ "Workspace and agent status are unavailable"
    refute html =~ "No artifacts"
    refute has_element?(view, "form")
    refute has_element?(view, "[phx-click]")
    refute html =~ ~r/\sstyle\s*=/i
  end

  test "missing selection trust and foreign UUID are explicit without source leaks", c do
    {:ok, _, html} = live(get(c.conn, "https://localhost/dashboard/spec-kit"))
    assert html =~ "No workspace selected"
    {:ok, _, html} = live(get(c.conn, url(c.workspace.id)))
    assert html =~ "Existing revision trust is unavailable"
    {:ok, _, html} = live(get(c.conn, url(Ecto.UUID.generate())))
    assert html =~ "Workspace not found"
    refute html =~ "private-repository"
    foreign_owner = Repo.insert!(%WorkHarbor.Foundation.Owner{display_name: "Foreign"})
    foreign_host = Repo.insert!(%Host{owner_id: foreign_owner.id})

    foreign =
      Repo.insert!(%Workspace{
        owner_id: foreign_owner.id,
        host_id: foreign_host.id,
        desired_state: "stopped",
        requested_config: DB.config()
      })

    {:ok, _, foreign_html} = live(get(c.conn, url(foreign.id)))
    assert foreign_html =~ "Workspace not found"
    refute foreign_html =~ "private-repository"
    refute foreign_html =~ foreign.id
  end

  test "dashboard rejects query overrides duplicate keys hostile origins form posts and rebinding",
       c do
    for extra <-
          ~w(path url repository repository_identity commit owner owner_id source_id feature artifact) do
      assert get(c.conn, url(c.workspace.id) <> "&#{extra}=CANARY").status == 400
    end

    assert get(c.conn, url(c.workspace.id) <> "&workspace_id=#{c.workspace.id}").status == 400
    assert get(c.conn, "https://localhost/dashboard?workspace_id=#{c.workspace.id}").status == 400

    for path <- ["/dashboard", "/dashboard/spec-kit", "/dashboard/live/websocket"] do
      response =
        c.conn
        |> put_req_header("origin", "https://attacker.test")
        |> get("https://localhost" <> path)

      assert response.status == 403
      refute response.resp_body =~ c.workspace.id
    end

    assert c.conn
           |> put_req_header("origin", "https://attacker.test")
           |> get(url(c.workspace.id))
           |> Map.fetch!(:status) == 403

    assert %{c.conn | req_headers: [{"host", "attacker.test"}]}
           |> get(url(c.workspace.id))
           |> Map.fetch!(:status) == 403

    response =
      c.conn
      |> put_req_header("origin", "https://attacker.test")
      |> put_req_header("content-type", "application/x-www-form-urlencoded")
      |> post("https://localhost/dashboard/spec-kit", "workspace_id=#{c.workspace.id}")

    assert response.status == 403
  end

  test "dashboard credential cannot call implemented mutation routes and denials are audited",
       c do
    before = Repo.aggregate(WorkHarbor.Accounts.Credential, :count)

    domain_before =
      {Repo.all(Host), Repo.all(Workspace), Repo.all(WorkHarbor.Foundation.Operation),
       Repo.all(WorkHarbor.ConfigTrust.Approval)}

    for path <- [
          "/api/v1/auth/login",
          "/owner/logout",
          "/owner/dashboard",
          "/api/v1/auth/logout",
          "/api/v1/auth/revoke",
          "/api/v1/auth/dashboard-credentials",
          "/api/v1/auth/dashboard-install",
          "/api/v1/hosts/enrollments",
          "/api/v1/hosts/#{c.workspace.host_id}/revoke"
        ] do
      conn = base_conn() |> put_req_header("authorization", "Bearer " <> c.token)
      assert post(conn, "https://localhost" <> path).status == 403
    end

    assert Repo.aggregate(WorkHarbor.Accounts.Credential, :count) == before

    assert {Repo.all(Host), Repo.all(Workspace), Repo.all(WorkHarbor.Foundation.Operation),
            Repo.all(WorkHarbor.ConfigTrust.Approval)} == domain_before

    assert Repo.query!("SELECT count(*) FROM audit_events WHERE event='authorization_denied'", [],
             log: false
           ).rows == [[9]]
  end

  test "forged events neither mutate nor log and revoked mounted credentials stop once", c do
    previous_level = Logger.level()
    Logger.configure(level: :debug)
    on_exit(fn -> Logger.configure(level: previous_level) end)

    log =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        {:ok, view, _} = live(get(c.conn, url(c.workspace.id)))
        before = Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count)
        render_hook(view, "approve", %{"commit" => "FORGED-PAYLOAD-CANARY"})
        assert Process.alive?(view.pid)
        assert Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count) == before
        assert :ok = Accounts.revoke(c.cli, c.dashboard.credential_id)

        send(
          view.pid,
          {:dashboard_session, :sys.get_state(view.pid).socket.private.dashboard_auth.ref}
        )

        assert_redirect(view, "/login/dashboard")
      end)

    refute log =~ c.token
    refute log =~ "FORGED-PAYLOAD-CANARY"
  end

  test "missing workspace refresh does not repeat its denial",
       c do
    {:ok, view, _} = live(get(c.conn, url(Ecto.UUID.generate())))
    before = Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count)
    ref = :sys.get_state(view.pid).socket.private.dashboard_auth.ref
    send(view.pid, {:dashboard_session, ref})
    assert render(view) =~ "Workspace not found"
    assert Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count) == before
  end

  test "dashboard transport filters reserved and application events before channel dispatch", c do
    alias WorkHarborWeb.DashboardSocket
    page = get(c.conn, url(c.workspace.id))
    html = html_response(page, 200)
    [_, signed] = Regex.run(~r/data-phx-session="([^"]+)"/, html)
    [_, static] = Regex.run(~r/data-phx-static="([^"]*)"/, html)
    assert {:ok, %{id: id}} = Phoenix.LiveView.Static.verify_token(Endpoint, signed)
    topic = "lv:" <> id

    assert {:ok, transport} =
             DashboardSocket.connect(%{
               endpoint: Endpoint,
               transport: :websocket,
               options: [serializer: [{Phoenix.Socket.V2.JSONSerializer, "~> 2.0.0"}]],
               params: %{"vsn" => "2.0.0"},
               connect_info: %{session: get_session(page)}
             })

    assert {:ok, transport} = DashboardSocket.init(transport)

    join = %{
      "session" => signed,
      "static" => static,
      "url" => url(c.workspace.id),
      "params" => %{"_mounts" => 0}
    }

    wire = fn event, payload ->
      {Jason.encode!(["join", "ref", topic, event, payload]), [opcode: :text]}
    end

    assert {:reply, :ok, {:text, _}, transport} =
             DashboardSocket.handle_in(wire.("phx_join", join), transport)

    {state, _} = transport
    {pid, _, :joined} = Map.fetch!(state.channels, topic)
    monitor = Process.monitor(pid)

    :sys.replace_state(pid, fn state ->
      %{state | socket: Phoenix.LiveView.put_flash(state.socket, :info, "FLASH-CANARY")}
    end)

    before = Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count)

    log =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        for event <- [
              "event",
              "lv:clear-flash",
              "link",
              "redirect",
              "allow_upload",
              "progress",
              "FORGED-EVENT-CANARY"
            ],
            payload <- [
              %{"event" => "lv:clear-flash", "value" => %{"commit" => "FORGED-PAYLOAD-CANARY"}},
              nil,
              false,
              7
            ] do
          assert {:ok, ^transport} = DashboardSocket.handle_in(wire.(event, payload), transport)
        end
      end)

    assert :ok = Phoenix.LiveView.Channel.ping(pid)
    assert :sys.get_state(pid).socket.assigns.flash["info"] == "FLASH-CANARY"
    assert Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count) == before
    refute log =~ "FORGED-PAYLOAD-CANARY"
    refute log =~ "FORGED-EVENT-CANARY"
    assert {:ok, _} = DashboardSocket.handle_in(wire.("phx_leave", %{}), transport)
    assert_receive {:DOWN, ^monitor, :process, ^pid, _}, 1000
  end

  test "hostile stored source identity is inert and visibly escapes controls", c do
    identity = "git@example.test:<b>x</b>\u202Eevil.git"
    descriptor = Map.put(DB.config(), "repository_identity", identity)

    Repo.update_all(from(w in Workspace, where: w.id == ^c.workspace.id),
      set: [requested_config: descriptor]
    )

    assert {:ok, _} = ConfigTrust.approve(c.cli, descriptor)
    {:ok, view, html} = live(get(c.conn, url(c.workspace.id)))
    assert html =~ "&lt;b&gt;x&lt;/b&gt;"
    assert html =~ "&lt;U+202E&gt;"
    refute html =~ "\u202E"
    refute has_element?(view, "b")
  end

  test "stale dashboard ticks neither read nor reschedule", c do
    {:ok, view, _} = live(get(c.conn, url(c.workspace.id)))
    old_ref = :sys.get_state(view.pid).socket.private.dashboard_auth.ref
    send(view.pid, {:dashboard_session, old_ref})
    assert render(view) =~ "Artifacts unavailable"
    current = :sys.get_state(view.pid).socket.private.dashboard_auth
    assert current.ref != old_ref
    Process.cancel_timer(current.timer)
    parent = self()
    telemetry_ref = make_ref()

    :ok =
      :telemetry.attach(
        telemetry_ref,
        Repo.config()[:telemetry_prefix] ++ [:query],
        fn _, _, metadata, _ -> send(parent, {telemetry_ref, metadata.query}) end,
        nil
      )

    try do
      for stale <- [old_ref, make_ref()], do: send(view.pid, {:dashboard_session, stale})
      assert render(view) =~ "Artifacts unavailable"
      assert :sys.get_state(view.pid).socket.private.dashboard_auth.ref == current.ref
      refute_receive {^telemetry_ref, _}, 25
    after
      :telemetry.detach(telemetry_ref)
    end
  end

  test "dashboard mount itself rejects owner and CLI credentials", c do
    socket = %Phoenix.LiveView.Socket{
      endpoint: Endpoint,
      view: WorkHarborWeb.SpecKitLive,
      router: WorkHarborWeb.Router,
      private: %{lifecycle: %Phoenix.LiveView.Lifecycle{}}
    }

    assert {:cont, admitted} =
             WorkHarborWeb.DashboardAuth.on_mount(
               :default,
               %{},
               %{"credential" => c.token},
               socket
             )

    assert admitted.private.dashboard_auth.principal == c.dashboard

    for kind <- ["owner_browser", "cli_owner"] do
      {:ok, token, _} = Accounts.login(c.cli.owner_id, c.secret, kind)

      assert {:halt, rejected} =
               WorkHarborWeb.DashboardAuth.on_mount(
                 :default,
                 %{},
                 %{"credential" => token},
                 socket
               )

      assert {:redirect, %{to: "/login/dashboard"}} = rejected.redirected
    end
  end

  test "absent workspace produces one denial across render mount and refresh", c do
    before = Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count)
    page = get(c.conn, url(Ecto.UUID.generate()))
    assert Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count) == before
    {:ok, view, _} = live(page)
    assert render(view) =~ "Workspace not found"
    assert Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count) == before + 1

    send(
      view.pid,
      {:dashboard_session, :sys.get_state(view.pid).socket.private.dashboard_auth.ref}
    )

    assert render(view) =~ "Workspace not found"
    assert Repo.aggregate(WorkHarbor.Foundation.AuditEvent, :count) == before + 1
  end

  test "owner and CLI credentials cannot mount dashboard", c do
    for kind <- ["owner_browser", "cli_owner"] do
      {:ok, token, principal} = Accounts.login(c.cli.owner_id, c.secret, kind)

      conn =
        if kind == "owner_browser",
          do: signed_in(token, principal),
          else: base_conn() |> put_req_header("authorization", "Bearer " <> token)

      assert get(conn, "https://localhost/dashboard/spec-kit").status in [401, 403]
    end
  end

  defp base_conn do
    conn = build_conn()
    %{conn | scheme: :https, host: "localhost", port: 443, req_headers: [{"host", "localhost"}]}
  end

  defp url(id), do: "https://localhost/dashboard/spec-kit?workspace_id=" <> id

  defp signed_in(token, principal) do
    conn = build_conn() |> Map.put(:secret_key_base, Endpoint.config(:secret_key_base))
    conn = BrowserSession.install(conn, :dashboard, token, principal.expires_at)
    cookie = conn.resp_cookies["__Host-workharbor_dashboard"].value
    base_conn() |> put_req_header("cookie", "__Host-workharbor_dashboard=" <> cookie)
  end
end
