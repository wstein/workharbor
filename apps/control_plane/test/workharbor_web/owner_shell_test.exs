# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerShellTest do
  use ExUnit.Case, async: false
  import Plug.Conn
  import Phoenix.ConnTest
  import Phoenix.LiveViewTest
  alias WorkHarbor.{Accounts, Repo}
  alias WorkHarbor.FoundationDatabase, as: DB
  alias WorkHarborWeb.{BrowserSession, CoreComponents, Endpoint, Layouts}
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
    {:ok, token, principal} = Accounts.login(owner, secret, "owner_browser")
    %{conn: signed_in(token, principal), principal: principal}
  end

  test "hostile flash text visibly escapes every FR022 control class and HTML" do
    controls =
      (Enum.to_list(0..31) -- [9]) ++
        Enum.to_list(127..159) ++
        [0x061C, 0x200E, 0x200F, 0x2028, 0x2029] ++
        Enum.to_list(0x202A..0x202E) ++
        Enum.to_list(0x2066..0x2069)

    for codepoint <- controls do
      control = <<codepoint::utf8>>
      payload = "before" <> control <> "after<script>CANARY</script>"

      html =
        render_component(&CoreComponents.flash/1,
          kind: :error,
          title: payload,
          flash: %{"error" => payload}
        )

      # Inspect the unique payload spans, leaving structural HEEx newlines alone.
      spans = Regex.scan(~r/before(.*?)after&lt;script&gt;CANARY&lt;\/script&gt;/su, html)
      assert length(spans) == 2

      for [_, replacement] <- spans do
        refute replacement =~ control, "unescaped U+#{Integer.to_string(codepoint, 16)}"
        refute replacement == "", "FR022 requires a visible placeholder"
      end

      refute html =~ "beforeafter"
      refute html =~ "<script>CANARY"
    end
  end

  test "hostile HTML attributes stay inert in shared error components" do
    payload = ~s(<img src=x onerror="CANARY">&"' <script>CANARY</script>)

    html =
      render_component(&CoreComponents.flash/1,
        kind: :error,
        title: payload,
        flash: %{"error" => payload},
        rest: %{"aria-label" => payload}
      )

    expected =
      "&lt;img src=x onerror=&quot;CANARY&quot;&gt;&amp;&quot;&#39; " <>
        "&lt;script&gt;CANARY&lt;/script&gt;"

    assert html =~ ~s(aria-label="#{expected}")
    assert html =~ "&quot;"
    assert html =~ "&#39;"
    refute html =~ "<img src=x"
    refute html =~ "<script>CANARY"
  end

  test "overview is a live read only shell with inactive future pages", c do
    {view, html} = mount_shell(c.conn)
    assert has_element?(view, "main")
    assert has_element?(view, "h1")
    assert has_element?(view, "nav[aria-label]")
    assert has_element?(view, ~s(a[href="/owner/auth"]))

    for path <- ["/owner/hosts", "/owner/workspaces", "/owner/trust"] do
      refute has_element?(view, ~s(a[href="#{path}"]))
      assert Phoenix.Router.route_info(WorkHarborWeb.Router, "GET", path, "localhost") == :error
    end

    assert html =~ "Unavailable"
    refute has_element?(view, "form")
    refute has_element?(view, "[data-method]")
    refute has_element?(view, ~s([phx-click*="push"]))
    refute html =~ "phx-submit"
    refute html =~ "phx-change"
  end

  test "owner routes permit only read pages and existing controller POST exceptions" do
    actual =
      WorkHarborWeb.Router.__routes__()
      |> Enum.filter(&(&1.path == "/" or String.starts_with?(&1.path, "/owner")))
      |> Enum.map(&{String.upcase(to_string(&1.verb)), &1.path})
      |> Enum.sort()

    assert actual ==
             Enum.sort([
               {"GET", "/"},
               {"GET", "/owner"},
               {"GET", "/owner/auth"},
               {"POST", "/owner/dashboard"},
               {"POST", "/owner/logout"}
             ])
  end

  test "forged events keep the page alive without state audit or payload logs", c do
    {view, _} = mount_shell(c.conn)
    before = database_snapshot()
    payload = %{"owner_id" => "FORGED-PAYLOAD-CANARY", "action" => "delete"}
    prior_level = Logger.level()
    Logger.configure(level: :debug)
    on_exit(fn -> Logger.configure(level: prior_level) end)

    logs =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        for event <- ["delete", "approve", "enroll", "lv:clear-flash", "unknown"] do
          render_hook(view, event, payload)
        end
      end)

    assert Process.alive?(view.pid)
    assert database_snapshot() == before
    refute logs =~ "FORGED-PAYLOAD-CANARY"
  end

  test "shell refresh performs no domain reads or writes", c do
    {view, _} = mount_shell(c.conn)
    before = database_snapshot()
    # The snapshot proves no writes; the source boundary test covers domain reads.
    receive do
    after
      2_100 -> :ok
    end

    assert render(view) =~ "Unavailable"
    assert database_snapshot() == before
  end

  test "owner page CSP is exact and contains no inline scripts or styles", c do
    page = get(c.conn, "https://localhost/owner")
    assert page.status == 200

    csp =
      "default-src 'none'; script-src 'self'; style-src 'self'; " <>
        "connect-src 'self' wss://localhost; img-src 'self'; form-action 'self'; " <>
        "frame-ancestors 'none'; base-uri 'none'"

    assert_security_headers(page, csp)
    html = html_response(page, 200)
    refute html =~ ~r/<script(?![^>]*\bsrc=)[^>]*>\s*[^<\s]/
    refute html =~ ~r/\sstyle\s*=/i
    refute html =~ "unsafe-inline"
    assert html =~ ~s(src="/assets/js/app.js")
    assert html =~ ~s(href="/assets/css/app.css")
  end

  test "owner shell icons obey strict CSP", c do
    {view, html} = mount_shell(c.conn)
    theme = render_component(&Layouts.theme_toggle/1, %{})

    for markup <- [html, theme] do
      refute markup =~ "data:image"
      refute markup =~ ~r/class="[^"]*\bhero-/
    end

    for label <- ["System", "Light", "Dark"] do
      assert has_element?(view, ~s(button[aria-label="#{label}"]))
    end

    # Check the actual pipeline output, never a fixture or source-only substitute.
    bundle_path = Application.app_dir(:workharbor, "priv/static/assets/css/app.css")
    assert File.regular?(bundle_path), "generated owner CSS bundle is required evidence"
    css = File.read!(bundle_path)
    refute css =~ ~r/data\s*:\s*image/i

    for file <- ["core_components.ex", "layouts.ex"] do
      source = File.read!(Path.expand("../../lib/workharbor_web/components/" <> file, __DIR__))
      refute source =~ "data:image"
    end
  end

  test "read only page source excludes domain persistence raw HTML and event pushes" do
    for name <- ["owner_auth.ex", "overview_live.ex"] do
      path = Path.expand("../../lib/workharbor_web/live/" <> name, __DIR__)
      assert File.regular?(path), "the read-only shell requires #{name}"
      source = File.read!(path)
      refute source =~ ~r/\b(Repo|Hosts|Foundation|ConfigTrust|HostConnections)\b/
      refute source =~ ~r/\braw\s*\(|\|>\s*raw\b|\{\s*:safe\s*,/
      refute source =~ ~r/Accounts\.(revoke|deny|bootstrap|login|issue_dashboard)\s*\(/
      refute source =~ ~r/\b(push_event|JS\.push)\s*\(/
    end
  end

  defp mount_shell(conn) do
    page = get(conn, "https://localhost/owner")
    html = html_response(page, 200)
    assert html =~ "data-phx-session", "owner page must become a LiveView before connecting"
    [_, signed] = Regex.run(~r/data-phx-session="([^"]+)"/, html)
    assert {:ok, %{session: signed_session}} = Phoenix.LiveView.Static.verify_token(Endpoint, signed)
    refute Map.has_key?(signed_session, "credential")
    assert {:ok, view, connected_html} = live(page)
    {view, connected_html}
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
        req_headers: [{"host", "localhost"} | conn.req_headers]
    }

    put_req_header(conn, "cookie", "__Host-workharbor_owner=" <> cookie)
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

  defp database_snapshot do
    tables = [
      "owners",
      "hosts",
      "workspaces",
      "operations",
      "runtime_instances",
      "owner_credentials",
      "audit_events"
    ]

    Map.new(tables, fn table ->
      {table, DB.sql("SELECT * FROM " <> table <> " ORDER BY rowid").rows}
    end)
  end
end
