# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerShellTest do
  use ExUnit.Case, async: false
  import Plug.Conn
  import Phoenix.ConnTest
  import Phoenix.LiveViewTest
  alias WorkHarbor.{Accounts, Repo}
  alias WorkHarbor.FoundationDatabase, as: DB
  alias WorkHarborWeb.{BrowserSession, CoreComponents, Endpoint, Layouts, OwnerSocket}
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

  test "display controls use exact visible markers before HTML escaping" do
    text = "before" <> <<0, 13, 27, 127, 0xC2, 0x85>> <> "<script>CANARY</script>"

    assert CoreComponents.display_text(text) ==
             "before<U+0000><U+000D><U+001B><U+007F><U+0085><script>CANARY</script>"

    assert CoreComponents.display_text("kept\ttab") == "kept\ttab"
    html = render_component(&CoreComponents.flash/1, kind: :error, flash: %{"error" => text})

    assert html =~
             "before&lt;U+0000&gt;&lt;U+000D&gt;&lt;U+001B&gt;&lt;U+007F&gt;&lt;U+0085&gt;&lt;script&gt;CANARY&lt;/script&gt;"

    refute html =~ "<script>CANARY"
  end

  test "owner dot segments are refused without reflection or effects", c do
    before = database_snapshot()

    for path <- [
          "/owner/./auth",
          "/owner/../owner",
          "/owner/%2e/auth",
          "/owner/%2E%2E/owner",
          "/owner//auth",
          "/owner/../FORGED-PATH-CANARY"
        ] do
      conn = %{c.conn | request_path: path, path_info: String.split(path, "/", trim: true)}
      response = WorkHarborWeb.Security.call(conn, [])
      assert response.status == 400
      assert response.halted
      assert Jason.decode!(response.resp_body) == %{"error" => %{"code" => "invalid_request"}}
      refute response.resp_body =~ "FORGED-PATH-CANARY"
    end

    assert database_snapshot() == before
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

  test "flash extra attributes visibly escape control characters before HTML escaping" do
    for codepoint <- [0, 10, 13, 27, 127, 0x85, 0x202E, 0x2066] do
      marker = codepoint |> Integer.to_string(16) |> String.upcase() |> String.pad_leading(4, "0")
      payload = "before" <> <<codepoint::utf8>> <> "after\"<&CANARY"

      html =
        render_component(&CoreComponents.flash/1,
          kind: :error,
          flash: %{"error" => "message"},
          rest: %{"aria-label" => payload}
        )

      assert html =~ ~s(aria-label="before&lt;U+#{marker}&gt;after&quot;&lt;&amp;CANARY")
    end
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
        for event <- ["delete", "approve", "enroll", "unknown", "FORGED-EVENT-CANARY"] do
          render_hook(view, event, payload)
        end
      end)

    assert Process.alive?(view.pid)
    assert database_snapshot() == before
    refute logs =~ "FORGED-PAYLOAD-CANARY"
    refute logs =~ "FORGED-EVENT-CANARY"
  end

  test "owner transport drops application envelopes before channel dispatch" do
    topic = "lv:transport-probe"

    state = %{
      channels: %{topic => {self(), make_ref(), :joined}},
      channels_inverse: %{self() => {topic, "join-ref"}},
      max_channels_per_transport: 100
    }

    transport = {state, %Phoenix.Socket{serializer: Phoenix.Socket.V2.JSONSerializer}}
    before = database_snapshot()
    prior_level = Logger.level()
    Logger.configure(level: :debug)
    on_exit(fn -> Logger.configure(level: prior_level) end)

    logs =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        for target <- [topic, "lvu:transport-probe", "unknown:transport-probe", "phoenix"],
            event <- blocked_events(),
            payload <- forged_payloads() do
          assert_dropped_wires(transport, target, event, payload)
        end

        for {target, event} <- [
              {topic, "heartbeat"},
              {"phoenix", "phx_join"},
              {"phoenix", "phx_leave"},
              {"lvu:probe", "phx_join"},
              {"unknown:probe", "phx_leave"}
            ] do
          assert {:ok, ^transport} =
                   OwnerSocket.handle_in(
                     wire_message(target, event, %{"canary" => "FORGED-PAYLOAD-CANARY"}),
                     transport
                   )
        end
      end)

    refute_receive %Phoenix.Socket.Message{}, 50
    refute_receive {:socket_push, _, _}, 50
    assert database_snapshot() == before
    refute logs =~ "FORGED-PAYLOAD-CANARY"
    refute logs =~ "FORGED-EVENT-CANARY"
  end

  test "owner transport preserves protocol joins and protects mounted flash state", c do
    page = get(c.conn, "https://localhost/owner")
    html = html_response(page, 200)
    [_, signed] = Regex.run(~r/data-phx-session="([^"]+)"/, html)
    [_, static] = Regex.run(~r/data-phx-static="([^"]*)"/, html)
    assert {:ok, %{id: id}} = Phoenix.LiveView.Static.verify_token(Endpoint, signed)
    topic = "lv:" <> id

    assert {:ok, transport} =
             OwnerSocket.connect(%{
               endpoint: Endpoint,
               transport: :websocket,
               options: [serializer: [{Phoenix.Socket.V2.JSONSerializer, "~> 2.0.0"}]],
               params: %{"vsn" => "2.0.0"},
               connect_info: %{session: get_session(page)}
             })

    assert {:ok, transport} = OwnerSocket.init(transport)
    heartbeat = wire_message("phoenix", "heartbeat", %{}, nil, "heartbeat-ref")

    assert {:reply, :ok, {:text, encoded}, ^transport} =
             OwnerSocket.handle_in(heartbeat, transport)

    assert [nil, "heartbeat-ref", "phoenix", "phx_reply", %{"status" => "ok"}] =
             Jason.decode!(IO.iodata_to_binary(encoded))

    join = %{
      "session" => signed,
      "static" => static,
      "url" => "https://localhost/owner",
      "params" => %{"_mounts" => 0}
    }

    assert {:reply, :ok, {:text, joined}, transport} =
             OwnerSocket.handle_in(wire_message(topic, "phx_join", join), transport)

    assert ["join-ref", "event-ref", ^topic, "phx_reply", %{"status" => "ok"}] =
             Jason.decode!(IO.iodata_to_binary(joined))

    {state, _socket} = transport
    {pid, _monitor, :joined} = Map.fetch!(state.channels, topic)
    monitor = Process.monitor(pid)
    on_exit(fn -> if Process.alive?(pid), do: GenServer.stop(pid, :normal) end)

    :sys.replace_state(pid, fn state ->
      %{state | socket: Phoenix.LiveView.put_flash(state.socket, :info, "RESERVED-FLASH-CANARY")}
    end)

    before = database_snapshot()
    prior_level = Logger.level()
    Logger.configure(level: :debug)
    on_exit(fn -> Logger.configure(level: prior_level) end)

    logs =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        for event <- blocked_events(), payload <- forged_payloads() do
          assert_dropped_wires(transport, topic, event, payload)
        end

        for payload <- malformed_join_payloads() do
          assert_dropped_wires(transport, topic <> "-FORGED-EVENT-CANARY", "phx_join", payload)
        end

        # A valid payload isolates the empty-topic guard from join-shape checks.
        assert_dropped_wires(transport, "lv:", "phx_join", join)
        assert_dropped_wires(transport, "lv:", "phx_leave", %{})
        assert :ok = Phoenix.LiveView.Channel.ping(pid)
      end)

    assert Process.alive?(pid)
    assert :sys.get_state(pid).socket.assigns.flash["info"] == "RESERVED-FLASH-CANARY"
    assert database_snapshot() == before
    refute logs =~ "FORGED-PAYLOAD-CANARY"
    refute logs =~ "FORGED-EVENT-CANARY"

    assert {:ok, _transport} =
             OwnerSocket.handle_in(wire_message(topic, "phx_leave", %{}), transport)

    assert_receive {:DOWN, ^monitor, :process, ^pid, _reason}, 1_000
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

    flash =
      render_component(&CoreComponents.flash/1, kind: :info, flash: %{"info" => "Contrast check"})

    [dismiss] =
      Regex.run(~r/<button\b[^>]*class="notice-dismiss"[^>]*>.*?<\/button>/s, flash)

    assert dismiss =~ "<svg"
    refute dismiss =~ ~r/\bopacity-/
    refute dismiss =~ ~r/\sstyle\s*=/i

    for markup <- [html, theme, flash] do
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
      # Domain module references are forbidden; visible headings such as "Hosts" are text.
      refute source =~ ~r/\b(Repo|Hosts|Foundation|ConfigTrust|HostConnections)\s*\./

      refute source =~
               ~r/\b(alias|import|use)\s+WorkHarbor\.(\{[^}]*\b)?(Repo|Hosts|Foundation|ConfigTrust|HostConnections)\b/

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

    assert {:ok, %{session: signed_session}} =
             Phoenix.LiveView.Static.verify_token(Endpoint, signed)

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

  defp forged_payloads do
    for event <- ["delete", "unknown", "lv:clear-flash", "lv:FORGED-EVENT-CANARY"],
        payload <- [
          %{
            "event" => event,
            "type" => "hook",
            "value" => %{"secret" => "FORGED-PAYLOAD-CANARY"}
          },
          %{
            "event" => event,
            "type" => "form",
            "value" => "secret=FORGED-PAYLOAD-CANARY",
            "cid" => 123,
            "uploads" => %{"FORGED-PAYLOAD-CANARY" => []}
          },
          %{"event" => event},
          "FORGED-PAYLOAD-CANARY"
        ],
        do: payload
  end

  defp malformed_join_payloads do
    canary = "FORGED-PAYLOAD-CANARY"

    [
      canary,
      nil,
      false,
      7,
      [canary],
      %{},
      %{"session" => nil},
      %{"session" => [canary]},
      %{"session" => %{"canary" => canary}}
    ] ++
      for {key, value} <- [
            {"static", [canary]},
            {"url", %{"canary" => canary}},
            {"redirect", 7},
            {"params", canary}
          ] do
        %{"session" => canary, key => value}
      end
  end

  defp wire_message(topic, event, payload, join_ref \\ "join-ref", ref \\ "event-ref") do
    {Jason.encode!([join_ref, ref, topic, event, payload]), [opcode: :text]}
  end

  # Explicit negative classes are independent of the production allowlist.
  defp blocked_events do
    [
      "event",
      "link",
      "redirect",
      "phx_reply",
      "phx_error",
      "phx_close",
      "allow_upload",
      "progress",
      "live_patch",
      "cids_will_destroy",
      "cids_destroyed",
      "cids_FORGED-EVENT-CANARY",
      "FORGED-EVENT-CANARY"
    ]
  end

  defp assert_dropped_wires(transport, topic, event, payload) do
    encoded = Jason.encode!(payload)

    for {wire, opts} <- [
          wire_message(topic, event, payload),
          binary_message(topic, event, payload)
        ] do
      decoded = Phoenix.Socket.V2.JSONSerializer.decode!(wire, opts)

      assert %Phoenix.Socket.Message{
               topic: ^topic,
               event: ^event,
               join_ref: "join-ref",
               ref: "event-ref"
             } = decoded

      case opts[:opcode] do
        :text -> assert decoded.payload == payload
        :binary -> assert decoded.payload == {:binary, encoded}
      end

      assert {:ok, ^transport} = OwnerSocket.handle_in({wire, opts}, transport)
    end
  end

  defp binary_message(topic, event, payload) do
    # Client V2 push: kind, four one-byte metadata lengths, metadata, raw body.
    body = Jason.encode!(payload)

    frame =
      <<0, 8, 9, byte_size(topic), byte_size(event), "join-ref", "event-ref", topic::binary,
        event::binary, body::binary>>

    {frame, [opcode: :binary]}
  end
end
