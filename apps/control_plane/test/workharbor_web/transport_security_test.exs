# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.TransportSecurityTest do
  use ExUnit.Case, async: false
  alias WorkHarborWeb.{Endpoint, RuntimeSecurity}

  setup tags do
    dir = Path.join(System.tmp_dir!(), "workharbor-transport-#{Ecto.UUID.generate()}")
    File.mkdir!(dir)
    File.chmod!(dir, 0o700)

    certs =
      :public_key.pkix_test_data(%{
        root: [digest: :sha256, key: {:rsa, 2048, 65537}],
        peer: [
          digest: :sha256,
          key: {:rsa, 2048, 65537},
          extensions: [{:Extension, {2, 5, 29, 17}, false, [dNSName: ~c"localhost"]}]
        ]
      })

    certfile = Path.join(dir, "cert.pem")
    keyfile = Path.join(dir, "key.pem")
    File.write!(certfile, :public_key.pem_encode([{:Certificate, certs[:cert], :not_encrypted}]))

    File.write!(
      keyfile,
      :public_key.pem_encode([Tuple.insert_at(certs[:key], 2, :not_encrypted)])
    )

    File.chmod!(keyfile, 0o600)
    on_exit(fn -> File.rm_rf!(dir) end)

    ctx = %{
      dir: dir,
      certfile: certfile,
      keyfile: keyfile,
      cert: certs[:cert],
      cacerts: certs[:cacerts]
    }

    if tags[:tls], do: start_tls(ctx), else: ctx
  end

  test "owner client selects guarded socket without longpoll or embedded credentials" do
    source = File.read!(Path.expand("../../assets/js/app.js", __DIR__))
    assert source =~ ~r/new LiveSocket\(["']\/owner\/live["']/
    refute source =~ "longPollFallbackMs"
    refute source =~ "localStorage.setItem(\"credential"
    refute source =~ "sessionStorage.setItem(\"credential"
  end

  test "owner assets are served narrowly without loosening authentication policies" do
    csp =
      "default-src 'none'; script-src 'none'; form-action 'self'; " <>
        "frame-ancestors 'none'; base-uri 'none'"

    for path <- ["/assets/js/app.js", "/assets/css/app.css"] do
      conn =
        Plug.Test.conn(:get, "https://localhost" <> path)
        |> Plug.Conn.put_req_header("host", "localhost")

      response = Endpoint.call(conn, Endpoint.init([]))
      assert response.status == 200
      refute response.resp_body == ""
      refute response.resp_body =~ "data:image"
      assert_owner_asset_headers(response, csp)
    end

    for {path, statuses} <- [
          {"/images/logo.svg", [403, 404]},
          {"/robots.txt", [403, 404]},
          {"/favicon.ico", [403, 404]},
          {"/assets/js/app.js.map", [403, 404]},
          {"/assets/css/app.css.map", [403, 404]},
          {"/assets/js/app.js/extra", [403, 404]},
          {"/assets/../robots.txt", [403, 404]},
          {"/assets/%2e%2e/robots.txt", [400, 403, 404]},
          {"/assets/", [400, 403, 404]},
          {"/assets/js/", [400, 403, 404]}
        ] do
      conn =
        Plug.Test.conn(:get, "https://localhost" <> path)
        |> Plug.Conn.put_req_header("host", "localhost")

      response = Endpoint.call(conn, Endpoint.init([]))
      assert response.status in statuses, "unexpected asset admission for #{path}"
    end

    for path <- ["/login/owner", "/api/v1/auth/status", "/owner/auth"] do
      conn =
        Plug.Test.conn(:get, "https://localhost" <> path)
        |> Plug.Conn.put_req_header("host", "localhost")

      response = WorkHarborWeb.Security.call(conn, [])
      assert_owner_asset_headers(response, csp)
    end
  end

  defp assert_owner_asset_headers(conn, csp) do
    for {name, value} <- [
          {"cache-control", "no-store"},
          {"x-content-type-options", "nosniff"},
          {"x-frame-options", "DENY"},
          {"referrer-policy", "no-referrer"},
          {"strict-transport-security", "max-age=31536000"},
          {"content-security-policy", csp}
        ] do
      assert Plug.Conn.get_resp_header(conn, name) == [value]
    end
  end

  # Each profile is evaluated from the real runtime file; the listener tests use
  # these exact adapter child specs, so changing runtime protocol options is caught.
  defp runtime(ctx, env, port) do
    values = %{
      "PHX_SERVER" => "true",
      "WORKHARBOR_ORIGIN" => "https://localhost:#{port}",
      "WORKHARBOR_BIND" => "127.0.0.1",
      "WORKHARBOR_TLS_CERT_FILE" => ctx.certfile,
      "WORKHARBOR_TLS_KEY_FILE" => ctx.keyfile,
      "SECRET_KEY_BASE" => String.duplicate("s", 64),
      "DATABASE_PATH" => Path.join(ctx.dir, "prod.db")
    }

    prior = Map.new(values, fn {k, _} -> {k, System.get_env(k)} end)

    try do
      System.put_env(values)
      base = Config.Reader.read!(Path.expand("../../config/config.exs", __DIR__), env: env)
      cfg = Config.Reader.read!(Path.expand("../../config/runtime.exs", __DIR__), env: env)

      {Keyword.merge(base[:workharbor][Endpoint], cfg[:workharbor][Endpoint]),
       cfg[:workharbor][:owner_http]}
    after
      Enum.each(prior, fn {k, v} -> if v, do: System.put_env(k, v), else: System.delete_env(k) end)
    end
  end

  defp independent(ctx) do
    [
      server: true,
      adapter: Bandit.PhoenixAdapter,
      debug_errors: false,
      code_reloader: false,
      http: false,
      secret_key_base: String.duplicate("x", 64),
      https: [
        ip: {127, 0, 0, 1},
        port: 443,
        certfile: ctx.certfile,
        keyfile: ctx.keyfile,
        http_1_options: [enabled: true],
        http_2_options: [enabled: false]
      ]
    ]
  end

  defp refused(config) do
    assert_raise ArgumentError, "unsafe authentication server configuration", fn ->
      RuntimeSecurity.validate!(config,
        origin: "https://localhost",
        authorities: [{"localhost", 443}]
      )
    end
  end

  test "optional client peer verification permits only validated native issuer trio", c do
    dir =
      Path.join(
        WorkHarbor.HostFixture.physical_temp(),
        "whr-transport-issuer-#{Ecto.UUID.generate()}"
      )

    on_exit(fn -> File.rm_rf!(dir) end)
    {:ok, issuer} = WorkHarbor.Hosts.Issuer.initialize(dir)
    cfg = independent(c)

    trio = [
      cacerts: [issuer.certificate],
      verify: :verify_peer,
      fail_if_no_peer_cert: false,
      verify_fun: {&WorkHarbor.Hosts.TrustedIssuers.verify/3, dir}
    ]

    valid =
      Keyword.update!(cfg, :https, &(&1 ++ [thousand_island_options: [transport_options: trio]]))

    owner = [origin: "https://localhost", authorities: [{"localhost", 443}]]
    assert :ok = RuntimeSecurity.validate!(valid, owner)

    for options <- [
          Keyword.delete(trio, :cacerts),
          Keyword.delete(trio, :verify_fun),
          Keyword.put(trio, :verify_fun, {fn _, _, s -> {:valid, s} end, dir}),
          Keyword.put(trio, :verify_fun, {&WorkHarbor.Hosts.TrustedIssuers.verify/3, "relative"}),
          Keyword.put(trio, :verify, :verify_none),
          Keyword.put(trio, :cacerts, c.cacerts),
          trio ++ [verify: :verify_peer],
          trio ++ [alpn_preferred_protocols: ["h2"]],
          trio ++ [thousand_island_options: [handler_module: __MODULE__]]
        ] do
      refused(
        Keyword.update!(
          cfg,
          :https,
          &(&1 ++ [thousand_island_options: [transport_options: options]])
        )
      )
    end

    refused(Keyword.update!(cfg, :https, &(&1 ++ trio)))

    refused(
      Keyword.update!(
        cfg,
        :https,
        &(&1 ++
            [
              thousand_island_options: [
                transport_options: trio,
                handler_module: Bandit.HTTP2.Handler
              ]
            ])
      )
    )
  end

  test "active transport profile requires exact explicit protocol settings and adapter", c do
    cfg = independent(c)

    assert :ok =
             RuntimeSecurity.validate!(cfg,
               origin: "https://localhost",
               authorities: [{"localhost", 443}]
             )

    for {key, variants} <- [
          http_1_options: [
            nil,
            [],
            [enabled: false],
            [enabled: true, enabled: false],
            %{enabled: true}
          ],
          http_2_options: [
            nil,
            [],
            [enabled: true],
            [enabled: false, enabled: true],
            %{enabled: false}
          ]
        ],
        value <- variants do
      refused(Keyword.put(cfg, :https, Keyword.put(cfg[:https], key, value)))
    end

    for key <- [:http_1_options, :http_2_options] do
      refused(Keyword.put(cfg, :https, Keyword.delete(cfg[:https], key)))
      refused(Keyword.put(cfg, :https, [{key, cfg[:https][key]} | cfg[:https]]))
    end

    refused(Keyword.put(cfg, :adapter, nil))
    refused(Keyword.delete(cfg, :adapter))
    refused(Keyword.put(cfg, :adapter, __MODULE__))
    refused(Keyword.put(cfg, :https, %{http_2_options: [enabled: false]}))
    assert :ok = RuntimeSecurity.validate!([server: false], [])
  end

  test "listener activation requires explicit true or false before protocol validation", c do
    cfg = independent(c)
    unsafe = Keyword.put(cfg, :https, Keyword.put(cfg[:https], :http_2_options, enabled: true))

    for value <- [nil, :enabled, "true", 1, []] do
      refused(Keyword.put(unsafe, :server, value))
    end

    refused(Keyword.delete(unsafe, :server))
    refused([{:server, false} | unsafe])
    refused([{:server, true} | unsafe])
    assert :ok = RuntimeSecurity.validate!([server: false], [])
  end

  test "active transport profile refuses lower layer protocol and listener overrides", c do
    cfg = independent(c)

    for patch <- [
          [thousand_island_options: [handler_options: %{http_2_enabled: true}]],
          [thousand_island_options: [handler_module: Bandit.HTTP2.Handler]],
          [thousand_island_options: [transport_options: [alpn_preferred_protocols: ["h2"]]]],
          [thousand_island_options: [transport_module: ThousandIsland.Transports.TCP]],
          [scheme: :http],
          [plug: __MODULE__],
          [alpn_preferred_protocols: ["h2"]],
          [unknown_option: true]
        ] do
      refused(Keyword.put(cfg, :https, cfg[:https] ++ patch))
    end

    refused(Keyword.put(cfg, :https, [{:port, 8443} | cfg[:https]]))
    refused(Keyword.put(cfg, :https, Keyword.put(cfg[:https], :port, 0)))
  end

  test "real runtime activation is HTTP1 only in every authenticated environment", c do
    for env <- [:dev, :test, :prod] do
      {cfg, owner} = runtime(c, env, 443)
      assert cfg[:https][:http_1_options] == [enabled: true]
      assert cfg[:https][:http_2_options] == [enabled: false]

      assert cfg[:adapter] == Bandit.PhoenixAdapter
      assert :ok = RuntimeSecurity.validate!(cfg, owner)
    end
  end

  defp start_tls(c) do
    prior_owner = Application.fetch_env!(:workharbor, :owner_http)

    prior_endpoint =
      :ets.tab2list(Endpoint)
      |> Enum.filter(&(tuple_size(&1) == 2))
      |> Keyword.delete(:__config__)

    on_exit(fn ->
      Application.put_env(:workharbor, :owner_http, prior_owner)
      Endpoint.config_change([{Endpoint, prior_endpoint}], [])
    end)

    {runtime, _} = runtime(c, :test, 0)
    cfg = Application.fetch_env!(:workharbor, Endpoint) |> Keyword.merge(runtime)
    [spec] = Bandit.PhoenixAdapter.child_specs(Endpoint, Keyword.put(cfg, :otp_app, :workharbor))
    pid = start_supervised!(spec)
    {:ok, {{127, 0, 0, 1}, port}} = ThousandIsland.listener_info(pid)
    origin = "https://localhost:#{port}"
    owner = [origin: origin, authorities: [{"localhost", port}]]

    effective =
      cfg
      |> Keyword.put(:https, Keyword.put(cfg[:https], :port, port))
      |> Keyword.put(:url, scheme: "https", host: "localhost", port: port)

    assert :ok = RuntimeSecurity.validate!(effective, owner)
    Application.put_env(:workharbor, :owner_http, owner)
    Endpoint.config_change([{Endpoint, effective}], [])
    # Network request processes use the named repository. Share its isolated
    # sandbox transaction instead of relying on process-local dynamic_repo.
    WorkHarbor.Repo.put_dynamic_repo(WorkHarbor.Repo)
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(WorkHarbor.Repo)
    :ok = Ecto.Adapters.SQL.Sandbox.mode(WorkHarbor.Repo, {:shared, self()})
    {owner_id, secret} = WorkHarbor.AuthDatabase.fixture!()

    Map.merge(c, %{port: port, origin: origin, owner_id: owner_id, secret: secret})
  end

  defp connect(c, alpn, hostname \\ ~c"localhost") do
    opts = [
      mode: :binary,
      active: false,
      verify: :verify_peer,
      cacerts: c.cacerts,
      server_name_indication: hostname,
      customize_hostname_check: [match_fun: :public_key.pkix_verify_hostname_match_fun(:https)]
    ]

    opts = if alpn, do: Keyword.put(opts, :alpn_advertised_protocols, alpn), else: opts
    :ssl.connect({127, 0, 0, 1}, c.port, opts, 2000)
  end

  @tag :tls
  test "actual TLS negotiates only HTTP1 and verifies certificate identity", c do
    assert {:ok, sock} = connect(c, ["h2", "http/1.1"])

    try do
      assert {:ok, "http/1.1"} = :ssl.negotiated_protocol(sock)
      assert {:ok, c.cert} == :ssl.peercert(sock)
    after
      :ssl.close(sock)
    end

    assert {:error, _} = connect(c, ["http/1.1"], ~c"foreign.test")

    case connect(c, ["h2"]) do
      {:error, _} ->
        :ok

      {:ok, sock} ->
        try do
          refute :ssl.negotiated_protocol(sock) == {:ok, "h2"}
          assert :ok = :ssl.send(sock, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
          assert {:error, :closed} = :ssl.recv(sock, 0, 2000)
        after
          :ssl.close(sock)
        end
    end
  end

  @tag :tls
  test "actual TLS rejects HTTP2 prior knowledge with and without ALPN", c do
    for alpn <- [nil, ["http/1.1"]] do
      assert {:ok, sock} = connect(c, alpn)

      try do
        assert :ok = :ssl.send(sock, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
        assert {:error, :closed} = :ssl.recv(sock, 0, 2000)
      after
        :ssl.close(sock)
      end
    end
  end

  defp response(sock, data \\ "") do
    case :binary.split(data, "\r\n\r\n") do
      [head, body] ->
        [status | lines] = String.split(head, "\r\n")

        headers =
          Enum.map(lines, fn line ->
            [k, v] = String.split(line, ":", parts: 2)
            {String.downcase(k), String.trim(v)}
          end)

        code = status |> String.split(" ") |> Enum.at(1) |> String.to_integer()

        length =
          headers
          |> List.keyfind("content-length", 0, {"content-length", "0"})
          |> elem(1)
          |> String.to_integer()

        if code == 101 or byte_size(body) >= length do
          %{status: code, headers: headers, body: body}
        else
          assert {:ok, more} = :ssl.recv(sock, 0, 2000)
          response(sock, data <> more)
        end

      _ ->
        assert {:ok, more} = :ssl.recv(sock, 0, 2000)
        response(sock, data <> more)
    end
  end

  defp request(c, method, path, headers \\ [], body \\ "") do
    assert {:ok, sock} = connect(c, ["http/1.1"])

    try do
      assert {:ok, "http/1.1"} = :ssl.negotiated_protocol(sock)

      headers = [
        {"host", "localhost:#{c.port}"},
        {"content-length", "#{byte_size(body)}"} | headers
      ]

      wire =
        method <>
          " " <>
          path <>
          " HTTP/1.1\r\n" <>
          Enum.map_join(headers, "", fn {k, v} -> k <> ": " <> v <> "\r\n" end) <> "\r\n" <> body

      assert :ok = :ssl.send(sock, wire)
      response(sock)
    after
      :ssl.close(sock)
    end
  end

  defp cookie(r, name) do
    {_, value} =
      Enum.find(r.headers, fn {k, v} ->
        k == "set-cookie" and String.starts_with?(v, name <> "=")
      end)

    value |> String.split(";", parts: 2) |> hd()
  end

  defp csrf(r), do: Regex.run(~r/name="_csrf_token" value="([^"]+)"/, r.body) |> Enum.at(1)

  defp browser(c, kind) do
    login = request(c, "GET", "/login/" <> kind)
    assert login.status == 200

    form =
      URI.encode_query(%{
        "owner_id" => c.owner_id,
        "owner_secret" => c.secret,
        "_csrf_token" => csrf(login)
      })

    signed =
      request(
        c,
        "POST",
        "/login/" <> kind,
        [
          {"content-type", "application/x-www-form-urlencoded"},
          {"origin", c.origin},
          {"cookie", cookie(login, "__Host-workharbor_login")}
        ],
        form
      )

    assert signed.status == 303
    selected = cookie(signed, "__Host-workharbor_" <> kind)
    page = request(c, "GET", "/" <> kind, [{"cookie", selected}])
    assert page.status == 200
    {cookie(page, "__Host-workharbor_" <> kind), csrf(page)}
  end

  @tag :tls
  test "actual HTTP1 preserves browser form and CLI bearer authentication", c do
    {selected, token} = browser(c, "owner")

    assert request(c, "GET", "/api/v1/auth/status", [
             {"cookie", selected},
             {"x-workharbor-context", "owner"},
             {"origin", c.origin},
             {"x-csrf-token", token}
           ]).status == 200

    body = Jason.encode!(%{schema_version: 1, owner_id: c.owner_id, owner_secret: c.secret})
    login = request(c, "POST", "/api/v1/auth/login", [{"content-type", "application/json"}], body)
    assert login.status == 200
    bearer = Jason.decode!(login.body)["token"]

    assert request(c, "GET", "/api/v1/auth/status", [{"authorization", "Bearer " <> bearer}]).status ==
             200

    assert request(c, "GET", "/api/v1/auth/status").status == 401
  end

  defp upgrade(c, kind, csrf, selected, origin) do
    path =
      "/#{kind}/live/websocket?" <> URI.encode_query(%{"_csrf_token" => csrf, "vsn" => "2.0.0"})

    headers = [
      {"cookie", selected},
      {"connection", "upgrade"},
      {"upgrade", "websocket"},
      {"sec-websocket-key", Base.encode64(<<0::128>>)},
      {"sec-websocket-version", "13"}
    ]

    headers = if origin, do: [{"origin", origin} | headers], else: headers
    request(c, "GET", path, headers)
  end

  @tag :tls
  test "actual WSS preserves selected session CSRF origin and revocation gates", c do
    {own, own_csrf} = browser(c, "owner")
    {dash, dash_csrf} = browser(c, "dashboard")
    assert upgrade(c, "owner", own_csrf, own, c.origin).status == 101
    assert upgrade(c, "dashboard", dash_csrf, dash, c.origin).status == 101
    assert upgrade(c, "owner", own_csrf, own, nil).status == 403
    assert upgrade(c, "owner", own_csrf, own, "https://foreign.test").status == 403
    assert upgrade(c, "owner", dash_csrf, own, c.origin).status == 403
    assert upgrade(c, "owner", own_csrf, dash, c.origin).status == 403
    raw_cookie = own |> String.split("=", parts: 2) |> Enum.at(1) |> URI.decode()
    conn = %{Plug.Test.conn(:get, c.origin) | secret_key_base: Endpoint.config(:secret_key_base)}
    options = WorkHarborWeb.BrowserSession.options(:owner) |> Plug.Session.COOKIE.init()
    {_, session} = Plug.Session.COOKIE.get(conn, raw_cookie, options)
    {:ok, actor} = WorkHarbor.Accounts.authenticate(session["credential"], "owner_browser")
    assert :ok = WorkHarbor.Accounts.revoke(actor, actor.credential_id)
    assert upgrade(c, "owner", own_csrf, own, c.origin).status == 403
    assert upgrade(c, "dashboard", dash_csrf, dash, c.origin).status == 101
  end
end
