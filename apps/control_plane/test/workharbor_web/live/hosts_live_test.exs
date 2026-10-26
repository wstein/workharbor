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
    assert {:error, _} = Hosts.list_status(%{c.principal | kind: "dashboard"})
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
