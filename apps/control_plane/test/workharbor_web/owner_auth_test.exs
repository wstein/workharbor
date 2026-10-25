# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerAuthTest do
  use ExUnit.Case, async: false
  import Plug.Conn
  import Phoenix.ConnTest
  import Phoenix.LiveViewTest
  alias WorkHarbor.{Accounts, Repo}
  alias WorkHarbor.FoundationDatabase, as: DB
  alias WorkHarborWeb.{BrowserSession, Endpoint}
  @endpoint Endpoint

  setup tags do
    Repo.put_dynamic_repo(Repo)
    WorkHarbor.DataCase.setup_sandbox(tags)
    prior_clock = Application.get_env(:workharbor, :clock)
    Application.put_env(:workharbor, :clock, fn -> 2_000_000_000 end)

    on_exit(fn ->
      if prior_clock,
        do: Application.put_env(:workharbor, :clock, prior_clock),
        else: Application.delete_env(:workharbor, :clock)
    end)

    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    %{owner: owner, secret: secret}
  end

  test "root redirects owner to the read only overview", c do
    {conn, _} = signed_in(c, "owner_browser")
    assert redirected_to(get(conn, "https://localhost/"), 303) == "/owner"
  end

  test "only owner browser credentials admit the live shell", c do
    assert get(base_conn(), "https://localhost/owner").status in [401, 403]

    for kind <- ["dashboard", "cli_owner"] do
      {conn, _} = signed_in(c, kind)
      assert get(conn, "https://localhost/owner").status in [401, 403]
    end

    # The existing read-token login wraps this same dashboard kind, not a fourth kind.
    {:ok, read_token, read_principal} = Accounts.login(c.owner, c.secret, "dashboard")
    assert {:error, :forbidden} = Accounts.authorize(read_principal, :owner_page)
    wrong_kind = cookie_conn(:owner, read_token, read_principal)
    assert get(wrong_kind, "https://localhost/owner").status in [401, 403]

    {conn, _} = signed_in(c, "owner_browser")
    page = get(conn, "https://localhost/owner")
    assert html_response(page, 200) =~ "data-phx-session"
    assert {:ok, _view, _} = live(page)
  end

  test "expired and revoked owner credentials cannot mount", c do
    for invalid <- [:expired, :revoked] do
      {conn, p} = signed_in(c, "owner_browser")

      case invalid do
        :expired -> Application.put_env(:workharbor, :clock, fn -> p.expires_at end)
        :revoked -> assert :ok = Accounts.revoke(p, p.credential_id)
      end

      assert get(conn, "https://localhost/owner").status in [401, 403]
      Application.put_env(:workharbor, :clock, fn -> 2_000_000_000 end)
    end
  end

  test "mounted revocation stops at first denial without a repeating audit loop", c do
    {conn, p} = signed_in(c, "owner_browser")
    view = mount_shell(conn)
    monitor = Process.monitor(view.pid)
    assert :ok = Accounts.revoke(p, p.credential_id)
    before = denied_count()
    assert_redirect(view, "/login/owner", 1_500)
    assert_receive {:DOWN, ^monitor, :process, _, _}, 1_000
    after_denial = denied_count()
    assert after_denial in before..(before + 1)

    # Termination and a stable audit count prove the observable stop, not queued state.
    receive do
    after
      1_100 -> :ok
    end

    assert denied_count() == after_denial
    assert get(conn, "https://localhost/owner").status in [401, 403]
  end

  test "mounted expiry clears the shell and requires fresh authentication", c do
    {conn, p} = signed_in(c, "owner_browser")
    view = mount_shell(conn)
    Application.put_env(:workharbor, :clock, fn -> p.expires_at end)
    before = denied_count()
    assert_redirect(view, "/login/owner", 1_500)
    assert denied_count() in before..(before + 1)
    assert get(conn, "https://localhost/owner").status in [401, 403]
  end

  test "foreign owner query injection is refused before shell reads", c do
    {conn, _} = signed_in(c, "owner_browser")
    before = DB.count("audit_events")
    response = get(conn, "https://localhost/owner?owner_id=FOREIGN-OWNER-CANARY")
    assert response.status == 400
    refute response.resp_body =~ "FOREIGN-OWNER-CANARY"
    assert DB.count("audit_events") == before
  end

  test "management forms stay in controller and preserve dashboard cookie and logout", c do
    assert %{plug: WorkHarborWeb.AuthController} =
             Phoenix.Router.route_info(WorkHarborWeb.Router, "GET", "/owner/auth", "localhost")

    {conn, p} = signed_in(c, "owner_browser")
    page = get(conn, "https://localhost/owner/auth")
    html = html_response(page, 200)

    csp =
      "default-src 'none'; script-src 'none'; form-action 'self'; " <>
        "frame-ancestors 'none'; base-uri 'none'"

    assert_security_headers(page, csp)
    refute html =~ "data-phx-session"
    assert html =~ ~s(action="/owner/dashboard")
    assert html =~ ~s(action="/owner/logout")
    [_, csrf] = Regex.run(~r/name="_csrf_token" value="([^"]+)"/, html)
    cookie = page.resp_cookies["__Host-workharbor_owner"].value

    request =
      base_conn()
      |> put_req_header("cookie", "__Host-workharbor_owner=" <> cookie)
      |> put_req_header("origin", "https://localhost")
      |> put_req_header("content-type", "application/x-www-form-urlencoded")

    body = URI.encode_query(%{"_csrf_token" => csrf})
    installed = post(request, "https://localhost/owner/dashboard", body)
    assert installed.status == 303
    assert installed.resp_cookies["__Host-workharbor_dashboard"].http_only
    refute Map.has_key?(installed.resp_cookies, "__Host-workharbor_owner")
    logout = post(request, "https://localhost/owner/logout", body)
    assert redirected_to(logout, 303) == "/login/owner"
    assert logout.resp_cookies["__Host-workharbor_owner"].max_age == 0
    assert {:error, :forbidden} = Accounts.authorize(p, :owner_page)
  end

  defp mount_shell(conn) do
    page = get(conn, "https://localhost/owner")
    assert html_response(page, 200) =~ "data-phx-session"
    assert {:ok, view, _} = live(page)
    view
  end

  defp signed_in(c, kind) do
    {:ok, token, p} = Accounts.login(c.owner, c.secret, kind)

    if kind == "cli_owner" do
      {put_req_header(base_conn(), "authorization", "Bearer " <> token), p}
    else
      target = if kind == "owner_browser", do: :owner, else: :dashboard
      {cookie_conn(target, token, p), p}
    end
  end

  defp cookie_conn(target, token, p) do
    response =
      build_conn()
      |> Map.put(:secret_key_base, Endpoint.config(:secret_key_base))
      |> BrowserSession.install(target, token, p.expires_at)

    name = if target == :owner, do: "__Host-workharbor_owner", else: "__Host-workharbor_dashboard"
    put_req_header(base_conn(), "cookie", name <> "=" <> response.resp_cookies[name].value)
  end

  defp base_conn do
    conn = build_conn()

    %{
      conn
      | scheme: :https,
        host: "localhost",
        port: 443,
        req_headers: [{"host", "localhost"} | conn.req_headers]
    }
  end

  defp assert_security_headers(conn, csp) do
    for {name, value} <- [
          {"cache-control", "no-store"},
          {"x-content-type-options", "nosniff"},
          {"x-frame-options", "DENY"},
          {"referrer-policy", "no-referrer"},
          {"strict-transport-security", "max-age=31536000"},
          {"content-security-policy", csp}
        ] do
      assert get_resp_header(conn, name) == [value]
    end
  end

  defp denied_count do
    [[count]] =
      DB.sql("SELECT count(*) FROM audit_events WHERE event='authorization_denied'").rows

    count
  end
end
