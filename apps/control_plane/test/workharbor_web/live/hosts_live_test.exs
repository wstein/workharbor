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
    conn = build_conn()

    conn = %{
      conn
      | scheme: :https,
        host: "localhost",
        port: 443,
        req_headers: [{"host", "localhost"}]
    }

    put_req_header(conn, "cookie", "__Host-workharbor_owner=" <> cookie)
  end
end
