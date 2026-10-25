# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AuthenticationTest do
  use ExUnit.Case, async: false
  import Plug.Conn
  alias WorkHarborWeb.Endpoint
  alias WorkHarbor.FoundationDatabase, as: DB

  setup do
    path = Path.join(System.tmp_dir!(), "workharbor-http-auth-#{Ecto.UUID.generate()}.db")
    pid = WorkHarbor.AuthDatabase.open!(path)
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    Application.put_env(:workharbor, :clock, fn -> 2_000_000_000 end)

    on_exit(fn ->
      Application.delete_env(:workharbor, :clock)
      if Process.alive?(pid), do: GenServer.stop(pid)
      for s <- ["", "-wal", "-shm"], do: File.rm(path <> s)
    end)

    %{owner: owner, secret: secret}
  end

  defp req(method, path, body \\ "", headers \\ []) do
    conn =
      Plug.Test.conn(method, "https://localhost" <> path, body)

    conn = %{conn | req_headers: headers ++ [{"host", "localhost"} | conn.req_headers]}
    Endpoint.call(conn, Endpoint.init([]))
  end

  defp cookie(conn, name), do: conn.resp_cookies[name].value
  defp jar(pairs), do: Enum.map_join(pairs, "; ", fn {k, v} -> k <> "=" <> v end)

  defp csrf(conn) do
    match =
      Regex.run(~r/name="_csrf_token" value="([^"]+)"/, conn.resp_body) ||
        Regex.run(~r/name="csrf-token" content="([^"]+)"/, conn.resp_body)

    Enum.at(match, 1)
  end

  defp primary(c, kind) do
    page = req(:get, "/login/" <> kind)

    form =
      URI.encode_query(%{
        "owner_id" => c.owner,
        "owner_secret" => c.secret,
        "_csrf_token" => csrf(page)
      })

    req(:post, "/login/" <> kind, form, [
      {"content-type", "application/x-www-form-urlencoded"},
      {"origin", "https://localhost"},
      {"cookie", jar([{"__Host-workharbor_login", cookie(page, "__Host-workharbor_login")}])}
    ])
  end

  test "actual endpoint rejects plaintext wrong host and foreign origin before login", c do
    body = Jason.encode!(%{schema_version: 1, owner_id: c.owner, owner_secret: c.secret})

    http =
      Plug.Test.conn(:post, "http://localhost/api/v1/auth/login", body)
      |> put_req_header("content-type", "application/json")

    assert Endpoint.call(http, Endpoint.init([])).status == 403

    for h <- [
          {"host", "evil.test"},
          {"host", "localhost.evil"},
          {"origin", "https://evil.test"},
          {"origin", "null"},
          {"origin", "https://localhost/"}
        ],
        do:
          assert(
            req(:post, "/api/v1/auth/login", body, [{"content-type", "application/json"}, h]).status ==
              403
          )

    assert DB.count("owner_credentials") == 0
  end

  test "strict API login rejects alternate parsers duplicate fields and mixed credentials", c do
    body = Jason.encode!(%{schema_version: 1, owner_id: c.owner, owner_secret: c.secret})
    good = req(:post, "/api/v1/auth/login", body, [{"content-type", "application/json"}])
    assert good.status == 200
    assert %{"token" => token} = Jason.decode!(good.resp_body)

    for h <- [
          {"origin", "https://localhost"},
          {"sec-fetch-site", "same-origin"},
          {"x-workharbor-context", "owner"},
          {"authorization", "Bearer " <> token}
        ],
        do:
          assert(
            req(:post, "/api/v1/auth/login", body, [{"content-type", "application/json"}, h]).status in [
              400,
              403
            ]
          )

    for raw <- [
          "{}",
          ~s({"owner_id":"a","owner_id":"b"}),
          "[]",
          "",
          String.duplicate(" ", 1_048_577)
        ],
        do:
          assert(
            req(:post, "/api/v1/auth/login", raw, [{"content-type", "application/json"}]).status in [
              400,
              413
            ]
          )

    assert req(:post, "/api/v1/auth/login", body, [
             {"content-type", "application/x-www-form-urlencoded"}
           ]).status == 415
  end

  test "public cross site login creates preauth only and cookies deny scripts", c do
    owner = primary(c, "owner")
    dashboard = primary(c, "dashboard")
    assert owner.status == 303 and dashboard.status == 303
    page = req(:get, "/login/owner", "", [{"sec-fetch-site", "cross-site"}])
    assert page.status == 200
    assert Map.keys(page.resp_cookies) == ["__Host-workharbor_login"]
    refute page.resp_body =~ c.secret
    refute page.resp_body =~ c.owner
    refute page.resp_body =~ "<script"
    assert hd(get_resp_header(page, "content-security-policy")) =~ "script-src 'none'"

    for {conn, name} <- [
          {owner, "__Host-workharbor_owner"},
          {dashboard, "__Host-workharbor_dashboard"}
        ] do
      ck = conn.resp_cookies[name]
      assert ck.secure and ck.http_only and ck.same_site == "Strict"
      refute Map.has_key?(ck, :domain)
    end
  end

  test "copied signed preauth cookie expires exactly at ten minutes", c do
    page = req(:get, "/login/owner")

    form =
      URI.encode_query(%{
        "owner_id" => c.owner,
        "owner_secret" => c.secret,
        "_csrf_token" => csrf(page)
      })

    headers = [
      {"content-type", "application/x-www-form-urlencoded"},
      {"origin", "https://localhost"},
      {"cookie", jar([{"__Host-workharbor_login", cookie(page, "__Host-workharbor_login")}])}
    ]

    Application.put_env(:workharbor, :clock, fn -> 2_000_000_600 end)
    assert req(:post, "/login/owner", form, headers).status == 403
    Application.put_env(:workharbor, :clock, fn -> 2_000_000_599 end)
    assert req(:post, "/login/owner", form, headers).status == 303
  end

  test "both cookies never widen dashboard and read denial precedes malformed body", c do
    own = primary(c, "owner")
    dash = primary(c, "dashboard")

    cookies =
      jar([
        {"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")},
        {"__Host-workharbor_dashboard", cookie(dash, "__Host-workharbor_dashboard")}
      ])

    page = req(:get, "/dashboard", "", [{"cookie", cookies}])
    assert page.status == 200

    cookies =
      jar([
        {"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")},
        {"__Host-workharbor_dashboard", cookie(page, "__Host-workharbor_dashboard")}
      ])

    count = DB.count("owner_credentials")

    for p <- ["login", "logout", "dashboard-credentials", "dashboard-install", "revoke"],
        do:
          assert(
            req(:post, "/api/v1/auth/" <> p, "malformed", [
              {"cookie", cookies},
              {"x-workharbor-context", "dashboard"},
              {"origin", "https://localhost"}
            ]).status == 403
          )

    assert DB.count("owner_credentials") == count
    assert req(:get, "/api/v1/auth/status", "", [{"cookie", cookies}]).status == 400

    assert req(:get, "/api/v1/auth/status", "", [
             {"cookie", cookies},
             {"x-workharbor-context", "dashboard"},
             {"sec-fetch-site", "same-origin"},
             {"x-csrf-token", csrf(page)}
           ]).status == 200

    assert req(:get, "/owner", "", [{"cookie", cookies}, {"x-workharbor-context", "dashboard"}]).status ==
             403
  end

  test "socket and debugger paths cannot bypass ingress or session checks" do
    for p <- [
          "/owner/live/websocket",
          "/dashboard/live/websocket",
          "/__plug__/debugger/action",
          "/phoenix/live_reload/socket/websocket"
        ] do
      assert req(:get, p, "", [{"origin", "https://evil.test"}]).status == 403
      assert req(:get, p).status in [401, 403, 404]
    end

    assert Endpoint.config(:debug_errors) == false
    assert Endpoint.config(:code_reloader) == false
  end

  test "literal debugger POST never parses canary action or exposes session" do
    for headers <- [[], [{"origin", "https://evil.test"}], [{"host", "evil.test"}]] do
      conn =
        req(:post, "/__plug__/debugger/action", "action=SECRET-CANARY&session=SECRET-CANARY", [
          {"content-type", "application/x-www-form-urlencoded"} | headers
        ])

      assert conn.status in [401, 403, 404]
      refute conn.resp_body =~ "SECRET-CANARY"
    end

    conn = Plug.Test.conn(:post, "http://localhost/__plug__/debugger/action", "SECRET-CANARY")
    assert Endpoint.call(conn, Endpoint.init([])).status == 403
  end

  test "active authenticated profiles validate positive baseline then independent failures" do
    alias WorkHarborWeb.RuntimeSecurity
    dir = Path.join(System.tmp_dir!(), "workharbor-config-#{Ecto.UUID.generate()}")
    File.mkdir!(dir)
    File.chmod!(dir, 0o700)
    cert = Path.join(dir, "cert.pem")
    key = Path.join(dir, "key.pem")
    File.write!(cert, "test certificate configuration fixture")
    File.write!(key, "test private key configuration fixture")
    File.chmod!(key, 0o600)
    on_exit(fn -> File.rm_rf!(dir) end)

    config = [
      server: true,
      adapter: Bandit.PhoenixAdapter,
      debug_errors: false,
      code_reloader: false,
      http: false,
      secret_key_base: String.duplicate("x", 64),
      https: [
        ip: {127, 0, 0, 1},
        port: 443,
        certfile: cert,
        keyfile: key,
        http_1_options: [enabled: true],
        websocket_options: [max_fragmented_message_size: 65_536],
        http_2_options: [enabled: false]
      ]
    ]

    owner = [origin: "https://localhost", authorities: [{"localhost", 443}]]
    assert :ok == RuntimeSecurity.validate!(config, owner)

    demos = [
      "9kwSZZ3zqgPLCQHHHC7Q9KYXXSVttc/mmuUT4ZmFiV1BGre/cOXzP7jNrsMeH4N4",
      "4XBsLMaWd4lPmLPEtg7KgIs8803lJErmCoNVLqsywrCNiiRCgTgMU4U5WdOI+TAl"
    ]

    for patch <-
          [
            [debug_errors: true],
            [code_reloader: true],
            [http: [port: 4000]],
            [https: nil],
            [secret_key_base: "demo"],
            [force_ssl: [rewrite_on: [:x_forwarded_proto]]]
          ] ++ Enum.map(demos, &[secret_key_base: &1]) do
      assert_raise ArgumentError, "unsafe authentication server configuration", fn ->
        RuntimeSecurity.validate!(Keyword.merge(config, patch), owner)
      end
    end

    for bad <- ["https://localhost:", "https://localhost/path", "https://localhost:8443"] do
      assert_raise ArgumentError, "unsafe authentication server configuration", fn ->
        RuntimeSecurity.validate!(config, origin: bad, authorities: [{"localhost", 443}])
      end
    end

    assert :ok == RuntimeSecurity.validate!([server: false], [])
  end

  test "socket admission accepts only initialized selected kind expiry revocation and CSRF", c do
    own = primary(c, "owner")
    dash = primary(c, "dashboard")
    owner_cookie = cookie(own, "__Host-workharbor_owner")
    dashboard_cookie = cookie(dash, "__Host-workharbor_dashboard")
    # The socket callback is the same one reached by Phoenix transport after its
    # cookie/CSRF extraction; a real Endpoint positive probe follows below.
    decoded = decode_cookie(:owner, owner_cookie)

    assert {:ok, _} =
             WorkHarborWeb.OwnerSocket.connect(%{}, %Phoenix.Socket{}, %{session: decoded})

    assert :error ==
             WorkHarborWeb.DashboardSocket.connect(%{}, %Phoenix.Socket{}, %{session: decoded})

    token = decoded["credential"]
    {:ok, p} = WorkHarbor.Accounts.authenticate(token)
    Application.put_env(:workharbor, :clock, fn -> p.expires_at end)

    assert :error ==
             WorkHarborWeb.OwnerSocket.connect(%{}, %Phoenix.Socket{}, %{session: decoded})

    Application.put_env(:workharbor, :clock, fn -> p.issued_at end)
    assert :ok == WorkHarbor.Accounts.revoke(p, p.credential_id)

    assert :error ==
             WorkHarborWeb.OwnerSocket.connect(%{}, %Phoenix.Socket{}, %{session: decoded})

    assert :error ==
             WorkHarborWeb.OwnerSocket.connect(%{}, %Phoenix.Socket{}, %{
               session: decode_cookie(:dashboard, dashboard_cookie)
             })

    assert :error == WorkHarborWeb.OwnerSocket.connect(%{}, %Phoenix.Socket{}, %{session: nil})
  end

  test "viewer wrapping initializes fresh dashboard CSRF without touching credentials or owner",
       c do
    own = primary(c, "owner")
    {:ok, _, actor} = WorkHarbor.Accounts.login(c.owner, c.secret, "cli_owner")
    {:ok, read, _} = WorkHarbor.Accounts.issue_dashboard(actor)
    login = req(:get, "/login/read-token")

    cookies =
      jar([
        {"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")},
        {"__Host-workharbor_login", cookie(login, "__Host-workharbor_login")}
      ])

    rows = DB.sql("SELECT * FROM owner_credentials ORDER BY id").rows
    form = URI.encode_query(%{"dashboard_token" => read, "_csrf_token" => csrf(login)})

    wrap =
      req(:post, "/login/read-token", form, [
        {"cookie", cookies},
        {"origin", "https://localhost"},
        {"content-type", "application/x-www-form-urlencoded"}
      ])

    assert wrap.status == 303
    refute Map.has_key?(wrap.resp_cookies, "__Host-workharbor_owner")

    assert decode_cookie(:dashboard, cookie(wrap, "__Host-workharbor_dashboard")) == %{
             "credential" => read
           }

    assert rows == DB.sql("SELECT * FROM owner_credentials ORDER BY id").rows

    dash =
      req(:get, "/dashboard", "", [
        {"cookie",
         jar([{"__Host-workharbor_dashboard", cookie(wrap, "__Host-workharbor_dashboard")}])}
      ])

    refute csrf(dash) == csrf(login)

    cookies =
      jar([
        {"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")},
        {"__Host-workharbor_dashboard", cookie(dash, "__Host-workharbor_dashboard")}
      ])

    headers = [
      {"cookie", cookies},
      {"x-workharbor-context", "dashboard"},
      {"origin", "https://localhost"}
    ]

    assert req(:get, "/api/v1/auth/status", "", [{"x-csrf-token", csrf(login)} | headers]).status ==
             403

    assert req(:get, "/api/v1/auth/status", "", [{"x-csrf-token", csrf(dash)} | headers]).status ==
             200

    clear =
      req(:post, "/login/dashboard/clear", URI.encode_query(%{"_csrf_token" => csrf(dash)}), [
        {"cookie", cookies},
        {"origin", "https://localhost"},
        {"content-type", "application/x-www-form-urlencoded"}
      ])

    assert clear.status == 303
    assert clear.resp_cookies["__Host-workharbor_dashboard"].max_age == 0
    refute Map.has_key?(clear.resp_cookies, "__Host-workharbor_owner")
    assert rows == DB.sql("SELECT * FROM owner_credentials ORDER BY id").rows
  end

  test "owner installation preserves own cookie before_send and initializes only dashboard CSRF",
       c do
    own = primary(c, "owner")

    page =
      req(:get, "/owner", "", [
        {"cookie", jar([{"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")}])}
      ])

    ownercookie = cookie(page, "__Host-workharbor_owner")

    install =
      req(:post, "/api/v1/auth/dashboard-install", "{}", [
        {"content-type", "application/json"},
        {"cookie", jar([{"__Host-workharbor_owner", ownercookie}])},
        {"x-workharbor-context", "owner"},
        {"origin", "https://localhost"},
        {"x-csrf-token", csrf(page)}
      ])

    assert install.status == 303
    refute Map.has_key?(install.resp_cookies, "__Host-workharbor_owner")
    dashboardcookie = cookie(install, "__Host-workharbor_dashboard")
    assert Map.keys(decode_cookie(:dashboard, dashboardcookie)) == ["credential"]

    cookies =
      jar([
        {"__Host-workharbor_owner", ownercookie},
        {"__Host-workharbor_dashboard", dashboardcookie}
      ])

    assert req(:get, "/api/v1/auth/status", "", [
             {"cookie", cookies},
             {"x-workharbor-context", "dashboard"},
             {"origin", "https://localhost"},
             {"x-csrf-token", csrf(page)}
           ]).status == 403

    dash = req(:get, "/dashboard", "", [{"cookie", cookies}])
    assert dash.status == 200
    refute csrf(dash) == csrf(page)

    assert {:ok, _} =
             WorkHarbor.Accounts.authenticate(decode_cookie(:owner, ownercookie)["credential"])
  end

  defp decode_cookie(context, value) do
    options = WorkHarborWeb.BrowserSession.options(context)

    conn = %{
      Plug.Test.conn(:get, "https://localhost")
      | secret_key_base: Endpoint.config(:secret_key_base)
    }

    {_, session} = Plug.Session.COOKIE.get(conn, value, Plug.Session.COOKIE.init(options))
    session
  end

  test "cookie rewrites never extend near expiry wrapped credentials", c do
    {:ok, _, actor} = WorkHarbor.Accounts.login(c.owner, c.secret, "cli_owner")
    {:ok, read, p} = WorkHarbor.Accounts.issue_dashboard(actor)
    Application.put_env(:workharbor, :clock, fn -> p.expires_at - 10 end)
    login = req(:get, "/login/read-token")

    wrap =
      req(
        :post,
        "/login/read-token",
        URI.encode_query(%{"dashboard_token" => read, "_csrf_token" => csrf(login)}),
        [
          {"cookie",
           jar([{"__Host-workharbor_login", cookie(login, "__Host-workharbor_login")}])},
          {"origin", "https://localhost"},
          {"content-type", "application/x-www-form-urlencoded"}
        ]
      )

    assert wrap.resp_cookies["__Host-workharbor_dashboard"].max_age <= 10

    page =
      req(:get, "/dashboard", "", [
        {"cookie",
         jar([{"__Host-workharbor_dashboard", cookie(wrap, "__Host-workharbor_dashboard")}])}
      ])

    assert page.status == 200
    assert page.resp_cookies["__Host-workharbor_dashboard"].max_age <= 10
  end

  test "logout unknown body never revokes and owner browser cannot become CLI bearer", c do
    {:ok, token, p} = WorkHarbor.Accounts.login(c.owner, c.secret, "cli_owner")
    before = DB.count("audit_events")

    assert req(:post, "/api/v1/auth/logout", ~s({"unexpected":true}), [
             {"authorization", "Bearer " <> token},
             {"content-type", "application/json"}
           ]).status == 400

    assert {:ok, _} = WorkHarbor.Accounts.authenticate(token)
    assert DB.count("audit_events") == before
    {:ok, browser, _} = WorkHarbor.Accounts.login(c.owner, c.secret, "owner_browser")

    assert req(:get, "/api/v1/auth/status", "", [{"authorization", "Bearer " <> browser}]).status ==
             401

    assert {:ok, _} = WorkHarbor.Accounts.authenticate(browser, "owner_browser")
    assert p.kind == "cli_owner"
  end

  test "credential URL query aliases and duplicates never reach login or echo", c do
    body = Jason.encode!(%{schema_version: 1, owner_id: c.owner, owner_secret: c.secret})

    for query <- [
          "owner_secret=SECRET-CANARY",
          "%74oken=SECRET-CANARY",
          "token=x&token=SECRET-CANARY",
          "owner_secret=ok&%6fwner_secret=SECRET-CANARY"
        ] do
      conn =
        req(:post, "/api/v1/auth/login?" <> query, body, [{"content-type", "application/json"}])

      assert conn.status == 400
      refute conn.resp_body =~ "SECRET-CANARY"
    end

    assert DB.count("owner_credentials") == 0
  end

  test "denial audit identifies each actual closed route action before malformed body", c do
    {:ok, token, _} = WorkHarbor.Accounts.login(c.owner, c.secret, "dashboard")

    for {path, action} <- [
          {"login", "primary_login"},
          {"logout", "logout"},
          {"revoke", "revoke"},
          {"dashboard-credentials", "issue_dashboard"},
          {"dashboard-install", "issue_dashboard"}
        ] do
      count = DB.count("audit_events")

      conn =
        req(:post, "/api/v1/auth/" <> path, "bad", [
          {"authorization", "Bearer " <> token},
          {"content-type", "application/json"}
        ])

      assert conn.status == 403
      assert DB.count("audit_events") == count + 1

      [[actual]] =
        DB.sql(
          "SELECT action FROM audit_events WHERE event='authorization_denied' ORDER BY rowid DESC LIMIT 1"
        ).rows

      assert actual == action
    end

    assert DB.count("owner_credentials") == 1
  end

  test "malformed authority empty ports duplicates userinfo and bounds reject" do
    for host <- [
          "localhost:",
          "localhost:0",
          "localhost:65536",
          "user@localhost",
          "localhost.",
          "localhost:443/path",
          "localhost:443?x=1",
          "[::1",
          "localhost:abc"
        ] do
      conn = Plug.Test.conn(:get, "https://localhost/login/owner")
      conn = %{conn | req_headers: [{"host", host}]}
      assert WorkHarborWeb.Security.call(conn, []).status == 403
    end
  end

  test "actual bracketed IPv6 authority and HTTP2 authority without Host are accepted" do
    previous = Application.fetch_env!(:workharbor, :owner_http)
    on_exit(fn -> Application.put_env(:workharbor, :owner_http, previous) end)

    Application.put_env(:workharbor, :owner_http,
      origin: "https://[::1]",
      authorities: [{"::1", 443}]
    )

    conn = Plug.Test.conn(:get, "https://[::1]/login/owner")
    conn = %{conn | host: "[::1]", req_headers: [{"host", "[::1]"}]}
    refute WorkHarborWeb.Security.call(conn, []).halted
    {adapter, data} = conn.adapter

    conn = %{
      conn
      | host: "::1",
        req_headers: [],
        adapter: {adapter, Map.put(data, :http_protocol, :"HTTP/2")}
    }

    refute WorkHarborWeb.Security.call(conn, []).halted
  end

  test "protected HTML cannot borrow anonymous login cross site navigation exception", c do
    owner = primary(c, "owner")
    cookies = jar([{"__Host-workharbor_owner", cookie(owner, "__Host-workharbor_owner")}])

    for site <- ["cross-site", "same-site"] do
      assert req(:get, "/owner", "", [{"cookie", cookies}, {"sec-fetch-site", site}]).status ==
               403
    end

    assert req(:get, "/owner", "", [{"cookie", cookies}, {"sec-fetch-site", "same-origin"}]).status ==
             200

    assert req(:get, "/login/owner", "", [{"sec-fetch-site", "cross-site"}]).status == 200
  end

  test "actual configured-origin socket handshake verifies selected CSRF and both cookies", c do
    own = primary(c, "owner")
    dash = primary(c, "dashboard")

    op =
      req(:get, "/owner", "", [
        {"cookie", jar([{"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")}])}
      ])

    dp =
      req(:get, "/dashboard", "", [
        {"cookie",
         jar([{"__Host-workharbor_dashboard", cookie(dash, "__Host-workharbor_dashboard")}])}
      ])

    cookies =
      jar([
        {"__Host-workharbor_owner", cookie(op, "__Host-workharbor_owner")},
        {"__Host-workharbor_dashboard", cookie(dp, "__Host-workharbor_dashboard")}
      ])

    previous = Application.fetch_env!(:workharbor, :owner_http)
    on_exit(fn -> Application.put_env(:workharbor, :owner_http, previous) end)

    Application.put_env(:workharbor, :owner_http,
      origin: "https://plane.test:8443",
      authorities: [{"plane.test", 8443}]
    )

    probe = fn path, token ->
      query = URI.encode_query(%{"_csrf_token" => token, "vsn" => "2.0.0"})
      conn = Plug.Test.conn(:get, "https://plane.test:8443" <> path <> "?" <> query)

      conn = %{
        conn
        | req_headers: [
            {"host", "plane.test:8443"},
            {"origin", "https://plane.test:8443"},
            {"cookie", cookies},
            {"connection", "upgrade"},
            {"upgrade", "websocket"},
            {"sec-websocket-key", Base.encode64(<<0::128>>)},
            {"sec-websocket-version", "13"}
          ]
      }

      Endpoint.call(conn, Endpoint.init([]))
    end

    assert probe.("/owner/live/websocket", csrf(op)).state == :upgraded
    assert probe.("/dashboard/live/websocket", csrf(dp)).state == :upgraded
    assert probe.("/dashboard/live/websocket", csrf(op)).status == 403
    assert probe.("/owner/live/websocket", csrf(dp)).status == 403

    {:ok, principal} =
      WorkHarbor.Accounts.authenticate(
        decode_cookie(:owner, cookie(op, "__Host-workharbor_owner"))["credential"]
      )

    Application.put_env(:workharbor, :clock, fn -> principal.expires_at end)
    assert probe.("/owner/live/websocket", csrf(op)).status == 403
    Application.put_env(:workharbor, :clock, fn -> principal.issued_at end)
    assert :ok = WorkHarbor.Accounts.revoke(principal, principal.credential_id)
    assert probe.("/owner/live/websocket", csrf(op)).status == 403
    assert probe.("/dashboard/live/websocket", csrf(dp)).state == :upgraded
  end

  test "owner browser token never authenticates as bearer independently", c do
    {:ok, token, _} = WorkHarbor.Accounts.login(c.owner, c.secret, "owner_browser")

    assert req(:get, "/api/v1/auth/status", "", [{"authorization", "Bearer " <> token}]).status ==
             401

    assert {:ok, _} = WorkHarbor.Accounts.authenticate(token, "owner_browser")
  end

  test "configured default HTTPS port and host case compare as canonical origin", c do
    previous = Application.fetch_env!(:workharbor, :owner_http)
    on_exit(fn -> Application.put_env(:workharbor, :owner_http, previous) end)

    Application.put_env(:workharbor, :owner_http,
      origin: "https://LOCALHOST:443",
      authorities: [{"localhost", 443}]
    )

    page = req(:get, "/login/owner", "", [{"origin", "https://localhost"}])
    assert page.status == 200

    form =
      URI.encode_query(%{
        "owner_id" => c.owner,
        "owner_secret" => c.secret,
        "_csrf_token" => csrf(page)
      })

    assert req(:post, "/login/owner", form, [
             {"origin", "https://localhost"},
             {"cookie",
              jar([{"__Host-workharbor_login", cookie(page, "__Host-workharbor_login")}])},
             {"content-type", "application/x-www-form-urlencoded"}
           ]).status == 303
  end

  test "upgrade errors never reflect attacker header canary", c do
    own = primary(c, "owner")

    page =
      req(:get, "/owner", "", [
        {"cookie", jar([{"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")}])}
      ])

    query = URI.encode_query(%{"_csrf_token" => csrf(page), "vsn" => "2.0.0"})

    conn =
      req(:get, "/owner/live/websocket?" <> query, "", [
        {"origin", "https://localhost"},
        {"cookie", jar([{"__Host-workharbor_owner", cookie(page, "__Host-workharbor_owner")}])},
        {"connection", "upgrade"},
        {"upgrade", "websocket"},
        {"sec-websocket-key", Base.encode64(<<0::128>>)},
        {"sec-websocket-version", "SECRET-CANARY"}
      ])

    assert conn.status in [400, 403]
    refute conn.resp_body =~ "SECRET-CANARY"
  end

  test "encoded and noncanonical login paths never select a different store or mint", c do
    page = req(:get, "/login/owner")

    form =
      URI.encode_query(%{
        "owner_id" => c.owner,
        "owner_secret" => c.secret,
        "_csrf_token" => csrf(page)
      })

    headers = [
      {"cookie", jar([{"__Host-workharbor_login", cookie(page, "__Host-workharbor_login")}])},
      {"origin", "https://localhost"},
      {"content-type", "application/x-www-form-urlencoded"}
    ]

    for path <- [
          "/login/%6fwner",
          "/login/read%2Dtoken",
          "/login/%64ashboard/clear",
          "/login//owner",
          "/login/owner/"
        ] do
      conn = req(:post, path, form, headers)
      assert conn.status == 400
      assert conn.resp_cookies == %{}
      refute conn.resp_body =~ c.secret
      assert DB.count("owner_credentials") == 0
    end
  end

  test "actual response logs database and audit never expose credential canaries", c do
    {:ok, token, _} = WorkHarbor.Accounts.login(c.owner, c.secret, "cli_owner")

    log =
      ExUnit.CaptureLog.capture_log([level: :debug], fn ->
        for {path, body, headers} <- [
              {"/api/v1/auth/login", ~S({"owner_secret":"SECRET-CANARY\q"}),
               [{"content-type", "application/json"}]},
              {"/api/v1/auth/login?token=SECRET-CANARY", "{}",
               [{"content-type", "application/json"}]},
              {"/__plug__/debugger/action", "action=SECRET-CANARY",
               [{"content-type", "application/x-www-form-urlencoded"}]},
              {"/api/v1/auth/logout", "{",
               [{"content-type", "application/json"}, {"authorization", "Bearer " <> token}]}
            ] do
          conn = req(:post, path, body, headers)
          for secret <- ["SECRET-CANARY", c.secret, token], do: refute(conn.resp_body =~ secret)
        end
      end)

    for secret <- ["SECRET-CANARY", c.secret, token] do
      refute log =~ secret
      refute inspect(DB.sql("SELECT * FROM audit_events").rows) =~ secret
      refute inspect(DB.sql("SELECT * FROM owner_credentials").rows) =~ secret
    end
  end

  test "preauth rejects future malformed or wrong interval timestamps while preserving cookies",
       c do
    login = req(:get, "/login/owner")
    original = decode_cookie(:preauth, cookie(login, "__Host-workharbor_login"))

    form =
      URI.encode_query(%{
        "owner_id" => c.owner,
        "owner_secret" => c.secret,
        "_csrf_token" => csrf(login)
      })

    for fields <- [
          %{"issued_at" => 2_000_000_001, "expires_at" => 2_000_000_601},
          %{"issued_at" => "bad"},
          %{"expires_at" => 2_000_000_601}
        ] do
      options = WorkHarborWeb.BrowserSession.options(:preauth)

      conn = %{
        Plug.Test.conn(:get, "https://localhost")
        | secret_key_base: Endpoint.config(:secret_key_base)
      }

      value =
        Plug.Session.COOKIE.put(
          conn,
          nil,
          Map.merge(original, fields),
          Plug.Session.COOKIE.init(options)
        )

      result =
        req(:post, "/login/owner", form, [
          {"origin", "https://localhost"},
          {"content-type", "application/x-www-form-urlencoded"},
          {"cookie", jar([{"__Host-workharbor_login", value}])}
        ])

      assert result.status == 403
      assert result.resp_cookies == %{}
    end

    assert DB.count("owner_credentials") == 0
  end

  test "malformed UTF8 effective Host fails closed without reflecting canary" do
    for host <- [<<91, 255, 58, 93>>, <<255, 58>> <> "SECRET-CANARY"] do
      conn = Plug.Test.conn(:post, "https://localhost/api/v1/auth/login", "SECRET-CANARY")

      conn = %{
        conn
        | host: host,
          req_headers: [{"host", host}, {"content-type", "application/json"}]
      }

      result = Endpoint.call(conn, Endpoint.init([]))
      assert result.status == 403
      refute result.resp_body =~ "SECRET-CANARY"
      assert DB.count("owner_credentials") == 0
    end
  end

  test "schema failure preserves consumed request adapter and never revokes", c do
    {:ok, token, _} = WorkHarbor.Accounts.login(c.owner, c.secret, "cli_owner")

    result =
      req(:post, "/api/v1/auth/logout", ~s({"unexpected":true}), [
        {"authorization", "Bearer " <> token},
        {"content-type", "application/json"}
      ])

    assert result.status == 400
    {_, adapter} = result.adapter
    assert adapter.req_body == ""
    assert {:ok, _} = WorkHarbor.Accounts.authenticate(token)
  end

  test "owner root and no script controls install read context and logout independently", c do
    owner = primary(c, "owner")

    headers = [
      {"cookie", jar([{"__Host-workharbor_owner", cookie(owner, "__Host-workharbor_owner")}])}
    ]

    root = req(:get, "/", "", headers)
    assert root.status == 303
    assert get_resp_header(root, "location") == ["/owner"]
    page = req(:get, "/owner/auth", "", headers)
    assert page.status == 200
    assert page.resp_body =~ "Sign out of owner management"
    assert page.resp_body =~ "Open read-only dashboard"
    owncookie = cookie(page, "__Host-workharbor_owner")

    headers = [
      {"origin", "https://localhost"},
      {"content-type", "application/x-www-form-urlencoded"},
      {"cookie", jar([{"__Host-workharbor_owner", owncookie}])}
    ]

    install =
      req(:post, "/owner/dashboard", URI.encode_query(%{"_csrf_token" => csrf(page)}), headers)

    assert install.status == 303
    refute Map.has_key?(install.resp_cookies, "__Host-workharbor_owner")
    dashcookie = cookie(install, "__Host-workharbor_dashboard")

    view =
      req(:get, "/dashboard", "", [{"cookie", jar([{"__Host-workharbor_dashboard", dashcookie}])}])

    assert view.resp_body =~ "Clear dashboard sign-in"

    result =
      req(:post, "/owner/logout", URI.encode_query(%{"_csrf_token" => csrf(page)}), headers)

    assert result.status == 303
    assert result.resp_cookies["__Host-workharbor_owner"].max_age == 0
    refute Map.has_key?(result.resp_cookies, "__Host-workharbor_dashboard")

    assert {:error, :unauthenticated} =
             WorkHarbor.Accounts.authenticate(decode_cookie(:owner, owncookie)["credential"])

    assert {:ok, _} =
             WorkHarbor.Accounts.authenticate(decode_cookie(:dashboard, dashcookie)["credential"])
  end

  test "browser issuance installs HttpOnly cookie and cannot expose raw credentials to scripts",
       c do
    own = primary(c, "owner")

    page =
      req(:get, "/owner", "", [
        {"cookie", jar([{"__Host-workharbor_owner", cookie(own, "__Host-workharbor_owner")}])}
      ])

    headers = [
      {"cookie", jar([{"__Host-workharbor_owner", cookie(page, "__Host-workharbor_owner")}])},
      {"x-workharbor-context", "owner"},
      {"origin", "https://localhost"},
      {"x-csrf-token", csrf(page)},
      {"content-type", "application/json"}
    ]

    count = DB.count("owner_credentials")
    denied = req(:post, "/api/v1/auth/dashboard-credentials", "{}", headers)
    assert denied.status == 403
    assert DB.count("owner_credentials") == count
    {:ok, token, _} = WorkHarbor.Accounts.login(c.owner, c.secret, "cli_owner")

    issued =
      req(:post, "/api/v1/auth/dashboard-credentials", "{}", [
        {"authorization", "Bearer " <> token},
        {"content-type", "application/json"}
      ])

    assert issued.status == 200
    assert %{"token" => read} = Jason.decode!(issued.resp_body)
    assert {:ok, %{kind: "dashboard"}} = WorkHarbor.Accounts.authenticate(read)
  end
end
