# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.HostAPITest do
  use ExUnit.Case, async: false
  alias WorkHarbor.{Accounts, HostFixture, Hosts}

  setup do
    dir = Path.join(HostFixture.physical_temp(), "whr-host-api-#{Ecto.UUID.generate()}")
    File.mkdir!(dir)
    File.chmod!(dir, 0o700)
    repo = WorkHarbor.HostDatabase.open!(Path.join(dir, "state.db"))
    {owner, secret} = WorkHarbor.AuthDatabase.fixture!()
    {:ok, token, principal} = Accounts.login(owner, secret, "cli_owner")
    previous = Application.get_env(:workharbor, :host_identity)
    issuer_dir = Path.join(dir, "issuer")
    {:ok, _} = WorkHarbor.Hosts.Issuer.initialize(issuer_dir)
    Application.put_env(:workharbor, :host_identity, issuer_dir: issuer_dir)

    on_exit(fn ->
      if previous,
        do: Application.put_env(:workharbor, :host_identity, previous),
        else: Application.delete_env(:workharbor, :host_identity)

      if Process.alive?(repo), do: GenServer.stop(repo)
      File.rm_rf!(dir)
    end)

    %{token: token, principal: principal, dir: dir, issuer_dir: issuer_dir}
  end

  defp request(method, path, body, token) do
    c = Plug.Test.conn(method, "https://localhost" <> path, Jason.encode!(body))

    c = %{
      c
      | req_headers: [
          {"host", "localhost"},
          {"content-type", "application/json"},
          {"authorization", "Bearer " <> token}
        ]
    }

    WorkHarborWeb.Endpoint.call(c, WorkHarborWeb.Endpoint.init([]))
  end

  test "explicit owner host endpoints issue revoke and confirm offboarding", c do
    issued =
      request(:post, "/api/v1/hosts/enrollments", %{schema_version: 1, host_id: nil}, c.token)

    assert issued.status == 200
    body = Jason.decode!(issued.resp_body)
    assert String.starts_with?(body["code"], "whe1_")
    host = body["host_id"]
    revoked = request(:post, "/api/v1/hosts/#{host}/revoke", %{schema_version: 1}, c.token)
    assert revoked.status == 200

    assert request(:post, "/api/v1/hosts/#{host}/revoke", %{schema_version: 1}, c.token).status ==
             200

    status = request(:get, "/api/v1/hosts/#{host}", %{}, c.token)
    assert status.status == 200
    assert Jason.decode!(status.resp_body)["revoked"] == true

    assert request(
             :post,
             "/api/v1/hosts/enrollments",
             %{schema_version: 1, host_id: host},
             c.token
           ).status == 400
  end

  test "dashboard cannot mint host enrollment or revoke even known host", c do
    {:ok, issued} = Hosts.issue_enrollment(c.principal, nil)
    {:ok, dashboard, _} = Accounts.issue_dashboard(c.principal)

    assert request(
             :post,
             "/api/v1/hosts/enrollments",
             %{schema_version: 1, host_id: nil},
             dashboard
           ).status == 403

    assert request(
             :post,
             "/api/v1/hosts/#{issued.host_id}/revoke",
             %{schema_version: 1},
             dashboard
           ).status == 403

    assert request(:get, "/api/v1/hosts/#{issued.host_id}", %{}, dashboard).status == 403
    assert {:ok, %{revoked: false}} = Hosts.status(c.principal, issued.host_id)
  end

  test "identity startup loads explicit initialized signer and never replaces missing state", c do
    cfg = Application.fetch_env!(:workharbor, WorkHarborWeb.Endpoint)

    assert {:ok, config} =
             WorkHarborWeb.RuntimeSecurity.identity_config(cfg, issuer_dir: c.issuer_dir)

    assert config[:https][:thousand_island_options][:transport_options][:verify] == :verify_peer

    assert config[:https][:thousand_island_options][:transport_options][:fail_if_no_peer_cert] ==
             false

    assert length(config[:https][:thousand_island_options][:transport_options][:cacerts]) == 1
    missing = Path.join(c.dir, "missing")

    assert {:error, :invalid_identity} =
             WorkHarborWeb.RuntimeSecurity.identity_config(cfg, issuer_dir: missing)

    refute File.exists?(missing)
  end

  test "failed owner body validation preserves consumed adapter state", c do
    result =
      request(
        :post,
        "/api/v1/hosts/enrollments",
        %{schema_version: 1, host_id: nil, extra: "SECRET_SENTINEL"},
        c.token
      )

    assert result.status == 400
    assert elem(result.adapter, 1).req_body == ""
  end
end
