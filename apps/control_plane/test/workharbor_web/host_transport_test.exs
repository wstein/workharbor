# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.HostTransportTest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Accounts, Hosts, HostFixture, HostConnections}
  alias WorkHarbor.Hosts.Issuer
  alias WorkHarborWeb.{Endpoint, RuntimeSecurity}
  @moduletag :tls
  @moduletag timeout: 120_000

  setup tags do
    dir = Path.join(HostFixture.physical_temp(), "whr-real-tls-#{Ecto.UUID.generate()}")
    File.mkdir!(dir)
    File.chmod!(dir, 0o700)

    previous =
      Map.new(
        [:host_identity, :clock, :owner_http, :host_transport_fixture_repo],
        &{&1, Application.get_env(:workharbor, &1)}
      )

    endpoint =
      :ets.tab2list(Endpoint)
      |> Enum.filter(&(tuple_size(&1) == 2))
      |> Keyword.delete(:__config__)

    on_exit(fn ->
      Enum.each(previous, fn {k, v} ->
        if v,
          do: Application.put_env(:workharbor, k, v),
          else: Application.delete_env(:workharbor, k)
      end)

      Endpoint.config_change([{Endpoint, endpoint}], [])
      File.rm_rf!(dir)
    end)

    agent = Path.expand("../host_agent", File.cwd!())
    executable = Path.join(dir, "transport-client")

    assert {_, 0} =
             System.cmd(
               "go",
               ["build", "-o", executable, "./internal/connection/testdata/transport_client"],
               cd: agent,
               stderr_to_stdout: true
             )

    if tags[:near_expiry],
      do:
        Application.put_env(:workharbor, :clock, fn ->
          System.system_time(:second) - 86400 + 15
        end)

    repo = WorkHarbor.HostDatabase.open!(Path.join(dir, "state.db"))
    on_exit(fn -> if Process.alive?(repo), do: GenServer.stop(repo) end)
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, _} = Accounts.login(owner, secret, "cli_owner")
    {:ok, principal} = Accounts.authenticate(token)
    issuer_dir = Path.join(dir, "issuer")
    {:ok, _} = Issuer.initialize(issuer_dir)
    Application.put_env(:workharbor, :host_identity, issuer_dir: issuer_dir)
    Application.put_env(:workharbor, :host_transport_fixture_repo, repo)
    start_supervised!({HostConnections, repo: repo})

    tls =
      :public_key.pkix_test_data(%{
        root: [digest: :sha256, key: {:namedCurve, :secp256r1}],
        peer: [
          digest: :sha256,
          key: {:namedCurve, :secp256r1},
          extensions: [{:Extension, {2, 5, 29, 17}, false, [dNSName: ~c"localhost"]}]
        ]
      })

    certfile = Path.join(dir, "server.pem")
    keyfile = Path.join(dir, "server-key.pem")
    server_ca = Path.join(dir, "server-ca.pem")
    File.write!(certfile, :public_key.pem_encode([{:Certificate, tls[:cert], :not_encrypted}]))
    File.write!(keyfile, :public_key.pem_encode([Tuple.insert_at(tls[:key], 2, :not_encrypted)]))
    File.chmod!(keyfile, 0o600)

    File.write!(
      server_ca,
      :public_key.pem_encode(Enum.map(tls[:cacerts], &{:Certificate, &1, :not_encrypted}))
    )

    File.chmod!(server_ca, 0o600)
    preflight = Path.join(dir, "server-preflight.json")
    File.write!(preflight, Jason.encode!(%{Dir: dir, ServerCA: server_ca}))
    File.chmod!(preflight, 0o600)

    assert {"server_trust_preflight_ok\n", 0} =
             System.cmd(executable, ["tls_preflight", preflight], stderr_to_stdout: true)

    cfg =
      Application.fetch_env!(:workharbor, Endpoint)
      |> Keyword.merge(
        server: true,
        adapter: Bandit.PhoenixAdapter,
        http: false,
        debug_errors: false,
        code_reloader: false,
        secret_key_base: String.duplicate("t", 64),
        https: [
          ip: {127, 0, 0, 1},
          port: 0,
          certfile: certfile,
          keyfile: keyfile,
          http_1_options: [enabled: true],
          http_2_options: [enabled: false]
        ]
      )

    {:ok, cfg} = RuntimeSecurity.identity_config(cfg, issuer_dir: issuer_dir)

    [spec] =
      Bandit.PhoenixAdapter.child_specs(
        WorkHarbor.HostTransportEndpoint,
        Keyword.put(cfg, :otp_app, :workharbor)
      )

    assert :ok = WorkHarborWeb.AgentLogFilter.install()
    listener = start_supervised!(spec)
    {:ok, {{127, 0, 0, 1}, port}} = ThousandIsland.listener_info(listener)
    origin = "https://localhost:#{port}"
    authority = [origin: origin, authorities: [{"localhost", port}]]
    effective = Keyword.update!(cfg, :https, &Keyword.put(&1, :port, port))
    assert :ok = RuntimeSecurity.validate!(effective, authority)
    Application.put_env(:workharbor, :owner_http, authority)
    Endpoint.config_change([{Endpoint, effective}], [])

    %{
      dir: dir,
      repo: repo,
      principal: principal,
      token: token,
      executable: executable,
      origin: origin,
      server_ca: server_ca,
      port: port,
      tls: tls
    }
  end

  defp tls_connect(c, peer \\ nil, protocols \\ ["h2", "http/1.1"]) do
    opts = [
      mode: :binary,
      active: false,
      verify: :verify_peer,
      cacerts: c.tls[:cacerts],
      server_name_indication: ~c"localhost",
      customize_hostname_check: [match_fun: :public_key.pkix_verify_hostname_match_fun(:https)]
    ]

    opts = if protocols, do: opts ++ [alpn_advertised_protocols: protocols], else: opts

    opts =
      if peer, do: opts ++ [cert: peer.certificate, key: {:PrivateKeyInfo, peer.key}], else: opts

    :ssl.connect({127, 0, 0, 1}, c.port, opts, 2000)
  end

  defp http(sock, c, path, extra \\ "") do
    assert :ok =
             :ssl.send(sock, "GET #{path} HTTP/1.1\r\nHost: localhost:#{c.port}\r\n#{extra}\r\n")

    response(sock, "", System.monotonic_time(:millisecond) + 2000)
  end

  defp response(sock, data, deadline) do
    case :binary.split(data, "\r\n\r\n") do
      [head, body] ->
        length =
          case Regex.run(~r/content-length:\s*(\d+)/i, head) do
            [_, n] -> String.to_integer(n)
            _ -> 0
          end

        if byte_size(body) >= length do
          [_, status] = Regex.run(~r/^HTTP\/1\.1 (\d+)/, head)
          {:ok, String.to_integer(status), body}
        else
          receive_response(sock, data, deadline)
        end

      _ ->
        receive_response(sock, data, deadline)
    end
  end

  defp receive_response(sock, data, deadline) do
    if byte_size(data) > 16384, do: flunk("bounded HTTP response exceeded fixture limit")

    case :ssl.recv(sock, 0, max(1, deadline - System.monotonic_time(:millisecond))) do
      {:ok, chunk} -> response(sock, data <> chunk, deadline)
      _ -> {:error, :transport_closed}
    end
  end

  defp drain_log_events(events) do
    receive do
      {:after_primary_log, event} -> drain_log_events([event | events])
    after
      0 -> Enum.reverse(events)
    end
  end

  defp input(c, code, dir \\ nil) do
    path = Path.join(c.dir, "input-#{Ecto.UUID.generate()}.json")

    File.write!(
      path,
      Jason.encode!(%{
        Dir: dir || Path.join(c.dir, "identity-#{Ecto.UUID.generate()}"),
        Endpoint: c.origin,
        ServerCA: c.server_ca,
        Code: code,
        Pending: false
      })
    )

    File.chmod!(path, 0o600)
    path
  end

  defp invoke(c, mode, input), do: System.cmd(c.executable, [mode, input], stderr_to_stdout: true)

  defp state(input),
    do:
      input
      |> File.read!()
      |> Jason.decode!()
      |> Map.fetch!("Dir")
      |> Path.join("identity.json")
      |> File.read!()
      |> Jason.decode!()

  defp enroll(c) do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    input = input(c, issued.code)
    result = invoke(c, "enroll", input)

    if result != {"enrolled\n", 0} do
      private = input |> File.read!() |> Jason.decode!()
      saved = Path.join(private["Dir"], "identity.json") |> File.read!() |> Jason.decode!()
      csr = Base.decode64!(saved["pending"]["csr"])
      csr_class = elem(Issuer.csr_public(csr, nil), 0)
      diagnostic = invoke(c, "diagnose", input)

      flunk(
        "fixed enrollment result #{inspect(result)}; committed identity count #{Hosts.one("SELECT count(*) AS n FROM host_identities", [])["n"]}; CSR class #{csr_class}; TLS/HTTP class #{inspect(diagnostic)}"
      )
    end

    {issued, input}
  end

  defp spawn_client(c, mode, input) do
    port =
      Port.open({:spawn_executable, c.executable}, [
        :binary,
        :exit_status,
        :stderr_to_stdout,
        args: [mode, input]
      ])

    {:os_pid, pid} = Port.info(port, :os_pid)
    on_exit(fn -> stop_owned(pid, c.executable) end)
    port
  end

  defp stop_owned(pid, executable) do
    case System.cmd("ps", ["-p", Integer.to_string(pid), "-o", "command="],
           stderr_to_stdout: true
         ) do
      {command, 0} ->
        if String.starts_with?(String.trim(command), executable <> " ") do
          System.cmd("kill", ["-TERM", Integer.to_string(pid)], stderr_to_stdout: true)

          wait(fn ->
            elem(System.cmd("kill", ["-0", Integer.to_string(pid)], stderr_to_stdout: true), 1) !=
              0
          end)
        end

      _ ->
        :ok
    end
  end

  defp await_line(port, line, timeout \\ 5000),
    do: await_buffer(port, line, System.monotonic_time(:millisecond) + timeout)

  defp await_buffer(port, line, deadline) do
    key = {__MODULE__, port}
    buffer = Process.get(key, "")
    parts = String.split(buffer, "\n")
    complete = Enum.drop(parts, -1)

    case Enum.find_index(complete, &(&1 == line)) do
      nil ->
        receive do
          {^port, {:data, data}} ->
            Process.put(key, buffer <> data)
            await_buffer(port, line, deadline)

          {^port, {:exit_status, status}} ->
            flunk("fixture exited before expected event: #{status}")
        after
          max(0, deadline - System.monotonic_time(:millisecond)) -> flunk("fixture event timeout")
        end

      index ->
        Process.put(key, Enum.drop(parts, index + 1) |> Enum.join("\n"))
        :ok
    end
  end

  defp wait(fun, n \\ 100) do
    cond do
      fun.() ->
        :ok

      n == 0 ->
        flunk("bounded fixture condition timeout")

      true ->
        Process.sleep(50)
        wait(fun, n - 1)
    end
  end

  test "unavailable endpoint is never evidence of server authorization refusal", c do
    {_, input} = enroll(c)
    private = input |> File.read!() |> Jason.decode!()
    file = Path.join(private["Dir"], "identity.json")
    state = file |> File.read!() |> Jason.decode!()
    File.write!(file, Jason.encode!(Map.put(state, "endpoint", "https://localhost:1")))
    assert {"fixture_failed\n", 1} = invoke(c, "probe", input)
  end

  test "genuine Go enrollment heartbeat rotation and revocation use verified native TLS", c do
    {issued, input} = enroll(c)
    old = state(input)
    assert {"connected\n", 0} = invoke(c, "once", input)

    assert Hosts.one("SELECT last_seen FROM hosts WHERE id=?", [issued.host_id])["last_seen"] !=
             nil

    assert {"renewed\n", 0} = invoke(c, "renew", input)
    current = state(input)
    refute old["active"]["identity_id"] == current["active"]["identity_id"]
    assert current["pending"] == nil
    old_dir = Path.join(c.dir, "old-identity")
    File.mkdir!(old_dir)
    File.chmod!(old_dir, 0o700)
    File.write!(Path.join(old_dir, "identity.json"), Jason.encode!(old))
    File.chmod!(Path.join(old_dir, "identity.json"), 0o600)
    old_input = input(c, "", old_dir)
    assert {"server_refused\n", 0} = invoke(c, "probe", old_input)
    assert :ok = Hosts.revoke(c.principal, issued.host_id)
    assert {"server_refused\n", 0} = invoke(c, "probe", input)
    assert {"fixture_failed\n", 1} = invoke(c, "renew", input)
    assert {:ok, %{revoked: true}} = Hosts.status(c.principal, issued.host_id)
  end

  test "genuine idle WSS closes on worker death and production client reconnects", c do
    {issued, input} = enroll(c)
    idle = spawn_client(c, "idle", input)
    await_line(idle, "ready")
    assert :ok = stop_supervised(HostConnections)
    await_line(idle, "closed", 2000)
    assert_receive {^idle, {:exit_status, 0}}, 2000
    server = start_supervised!({HostConnections, repo: c.repo})
    running = spawn_client(c, "run", input)
    await_line(running, "connected")
    entry = :sys.get_state(server).peers |> Map.values() |> hd()
    ref = :sys.get_state(server).hosts[issued.host_id]
    assert :ok = HostConnections.disconnect({server, ref})

    wait(fn ->
      current =
        Hosts.one("SELECT connection_id FROM host_connection_slots WHERE host_id=?", [
          issued.host_id
        ])["connection_id"]

      not is_nil(current) and current != entry.id and
        HostConnections.host_state(issued.host_id) == {:ok, "connected"}
    end)

    assert {:ok, "connected"} = HostConnections.host_state(issued.host_id)
    {:os_pid, pid} = Port.info(running, :os_pid)
    stop_owned(pid, c.executable)
    assert_receive {^running, {:exit_status, 0}}, 2000
  end

  test "genuine transport ping cannot renew application heartbeat lease", c do
    {issued, input} = enroll(c)
    idle = spawn_client(c, "idle", input)
    await_line(idle, "ready")
    assert {:ok, "connected"} = HostConnections.host_state(issued.host_id)
    await_line(idle, "closed", 32000)
    assert_receive {^idle, {:exit_status, 0}}, 2000
    assert {:ok, "unknown"} = HostConnections.host_state(issued.host_id)

    assert Hosts.one("SELECT state FROM host_connection_slots WHERE host_id=?", [issued.host_id])[
             "state"
           ] == "unknown"
  end

  @tag :near_expiry
  test "genuine activation crash recovers pending after retired predecessor expiry", c do
    {issued, input} = enroll(c)
    old = state(input)

    old_expiry =
      Issuer.inspect_certificate(Base.decode64!(old["active"]["certificate"])).not_after

    Application.delete_env(:workharbor, :clock)
    assert {"lost_ack\n", 73} = invoke(c, "stage_lost", input)
    crashed = state(input)
    assert crashed["active"] == old["active"]
    assert crashed["pending"]["identity_id"] != nil

    assert Hosts.one("SELECT status FROM host_identities WHERE id=?", [
             crashed["pending"]["identity_id"]
           ])["status"] == "active"

    wait(fn -> System.system_time(:second) >= old_expiry end, 400)
    assert {"renewed\n", 0} = invoke(c, "renew", input)
    recovered = state(input)
    assert recovered["active"]["identity_id"] == crashed["pending"]["identity_id"]
    assert recovered["pending"] == nil
    assert {"connected\n", 0} = invoke(c, "once", input)
    assert :ok = Hosts.revoke(c.principal, issued.host_id)
  end

  test "genuine optional client TLS preserves owner HTTP1 and rejects H2 and owner as host", c do
    {:ok, sock} = tls_connect(c)

    try do
      assert {:ok, "http/1.1"} = :ssl.negotiated_protocol(sock)

      assert {:ok, 200, _} =
               http(sock, c, "/api/v1/auth/status", "Authorization: Bearer #{c.token}\r\n")

      assert {:ok, 401, _} = http(sock, c, "/agent/v1/connect")

      assert {:ok, 403, _} =
               http(sock, c, "/agent/v1/connect", "Authorization: Bearer #{c.token}\r\n")
    after
      :ssl.close(sock)
    end

    for protocols <- [nil, ["http/1.1"], ["h2"]] do
      case tls_connect(c, nil, protocols) do
        {:ok, h2} ->
          try do
            refute :ssl.negotiated_protocol(h2) == {:ok, "h2"}
            assert :ok = :ssl.send(h2, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
            assert {:error, _} = :ssl.recv(h2, 0, 2000)
          after
            :ssl.close(h2)
          end

        {:error, _} ->
          assert protocols == ["h2"]
      end
    end
  end

  @tag :peer_negative
  test "genuine TLS rejects foreign issuer and unregistered signed identity", c do
    {csr, key} = HostFixture.csr_with_key()
    {:ok, issuer} = Hosts.issuer()
    {:ok, certificate} = Issuer.issue(issuer, csr, Ecto.UUID.generate(), :enrollment)
    path = input(c, "")

    values =
      path
      |> File.read!()
      |> Jason.decode!()
      |> Map.merge(%{
        "Certificate" => Base.encode64(certificate),
        "PrivateKey" => Base.encode64(:public_key.der_encode(:PrivateKeyInfo, key)),
        "IssuerCertificate" => Base.encode64(issuer.certificate)
      })

    File.write!(path, Jason.encode!(values))
    assert {"server_refused\n", 0} = invoke(c, "probe_cert", path)
    foreign = Path.join(c.dir, "foreign-issuer")
    {:ok, other} = Issuer.initialize(foreign)
    {:ok, certificate} = Issuer.issue(other, csr, Ecto.UUID.generate(), :enrollment)

    values =
      Map.merge(values, %{
        "Certificate" => Base.encode64(certificate),
        "IssuerCertificate" => Base.encode64(other.certificate)
      })

    File.write!(path, Jason.encode!(values))
    assert {"server_tls_unknown_ca\n", 0} = invoke(c, "probe_cert", path)
  end

  test "genuine pending identity has only activation authority and rotation revocation close live WSS",
       c do
    {issued, input} = enroll(c)
    idle = spawn_client(c, "idle", input)
    await_line(idle, "ready")
    assert {"staged\n", 0} = invoke(c, "stage_only", input)
    pending_input = input(c, "")
    source = input |> File.read!() |> Jason.decode!() |> Map.put("Pending", true)
    File.write!(pending_input, Jason.encode!(source))
    assert {"server_refused\n", 0} = invoke(c, "probe", pending_input)
    assert {"renewed\n", 0} = invoke(c, "renew", input)
    await_line(idle, "closed", 2000)
    assert_receive {^idle, {:exit_status, 0}}, 2000
    next = spawn_client(c, "idle", input)
    await_line(next, "ready")
    assert :ok = Hosts.revoke(c.principal, issued.host_id)
    await_line(next, "closed", 2000)
    assert_receive {^next, {:exit_status, 0}}, 2000
    assert {"server_refused\n", 0} = invoke(c, "probe", input)
  end

  test "genuine WSS enforces exact wire scope version JSON types and message budgets", c do
    capture = :workharbor_test_agent_capture

    assert :ok =
             :logger.add_handler(capture, WorkHarbor.HostLogCapture, %{
               level: :all,
               config: %{test: self()}
             })

    on_exit(fn -> :logger.remove_handler(capture) end)
    {_, input} = enroll(c)
    assert {"boundary_ack\n", 0} = invoke(c, "boundary", input)

    for {mode, code} <- [
          {"fragment", 1009},
          {"oversize", 1008},
          {"wrong_scope", 1008},
          {"wrong_version", 1008},
          {"duplicate", 1008},
          {"binary", 1008},
          {"invalid_utf8", 1007}
        ] do
      assert invoke(c, mode, input) == {"wire_refused_#{code}\n", 0}
    end

    Process.sleep(50)
    events = drain_log_events([])
    redacted = Enum.filter(events, &(&1[:msg] == {:string, "WorkHarbor agent transport refused"}))
    assert length(redacted) >= 2

    for event <- redacted do
      assert Enum.sort(Map.keys(event)) == [:level, :meta, :msg]
      assert Enum.all?(Map.keys(event.meta), &(&1 in [:pid, :time, :domain]))
    end

    refute Enum.any?(events, fn
             %{msg: {:report, %{label: label}}}
             when label in [{:gen_server, :terminate}, {:proc_lib, :crash}] ->
               true

             _ ->
               false
           end)
  end

  @tag :near_expiry
  @tag :peer_negative
  test "genuine idle client socket is forced closed at certificate expiry", c do
    {_, input} = enroll(c)
    Application.delete_env(:workharbor, :clock)
    idle = spawn_client(c, "idle", input)
    await_line(idle, "ready")
    await_line(idle, "closed", 17000)
    assert_receive {^idle, {:exit_status, 0}}, 2000
    saved = state(input)

    assert Issuer.inspect_certificate(Base.decode64!(saved["active"]["certificate"])).not_after <=
             System.system_time(:second)

    material = saved["active"]
    path = input(c, "")

    values =
      path
      |> File.read!()
      |> Jason.decode!()
      |> Map.merge(%{
        "Certificate" => material["certificate"],
        "PrivateKey" => material["private_key"],
        "IssuerCertificate" => material["issuer_certificate"]
      })

    File.write!(path, Jason.encode!(values))
    assert invoke(c, "probe_cert", path) in [{"server_tls_expired\n", 0}, {"server_refused\n", 0}]
  end

  @tag :near_expiry
  test "genuine first activation after predecessor expiry fails and owner code recovers same host",
       c do
    {issued, input} = enroll(c)
    old = state(input)
    expiry = Issuer.inspect_certificate(Base.decode64!(old["active"]["certificate"])).not_after
    Application.delete_env(:workharbor, :clock)
    assert {"staged\n", 0} = invoke(c, "stage_only", input)
    wait(fn -> System.system_time(:second) >= expiry end, 400)
    # The REAL server body must be classified final by the Go agent.
    assert {"refused_final\n", 0} = invoke(c, "renew", input)
    pending = state(input)

    assert Hosts.one("SELECT status FROM host_identities WHERE id=?", [
             pending["pending"]["identity_id"]
           ])["status"] == "pending"

    {:ok, reissued} = Hosts.issue_enrollment(c.principal, issued.host_id)
    recovered_input = input(c, reissued.code)
    assert {"enrolled\n", 0} = invoke(c, "enroll", recovered_input)
    recovered = state(recovered_input)
    assert recovered["host_id"] == issued.host_id
    assert {"connected\n", 0} = invoke(c, "once", recovered_input)
  end

  test "genuine production Go enrollment verifies server root and name before code transmission",
       c do
    {:ok, issuer} = Hosts.issuer()
    wrong_ca = Path.join(c.dir, "wrong-server-ca.pem")

    File.write!(
      wrong_ca,
      :public_key.pem_encode([{:Certificate, issuer.certificate, :not_encrypted}])
    )

    File.chmod!(wrong_ca, 0o600)

    for {kind, expected} <- [{"root", "tls_unknown_authority\n"}, {"name", "tls_wrong_name\n"}] do
      {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
      input = input(c, issued.code)
      values = input |> File.read!() |> Jason.decode!()

      values =
        if kind == "root",
          do: Map.put(values, "ServerCA", wrong_ca),
          else: Map.put(values, "Endpoint", "https://127.0.0.1:#{c.port}")

      File.write!(input, Jason.encode!(values))
      assert {"fixture_failed\n", 1} = invoke(c, "enroll", input)
      assert invoke(c, "diagnose", input) == {expected, 0}

      assert Hosts.one("SELECT consumed_at FROM host_enrollments WHERE host_id=?", [
               issued.host_id
             ])["consumed_at"] == nil

      assert Hosts.one("SELECT count(*) AS n FROM host_identities WHERE host_id=?", [
               issued.host_id
             ])["n"] == 0
    end
  end
end
