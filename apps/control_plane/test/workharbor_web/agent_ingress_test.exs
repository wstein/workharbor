# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AgentIngressTest do
  use ExUnit.Case, async: false
  import Plug.Conn
  import Plug.Test
  alias WorkHarborWeb.AgentIngress

  defmodule FaultAdapter do
    @moduledoc false
    def read_req_body(state, _opts), do: {:error, state.fault}
    def send_resp(state, _status, _headers, body), do: {:ok, body, state}
  end

  defp faulty(fault) do
    c = request(:post, "/agent/v1/enroll", "{}")
    %{c | adapter: {FaultAdapter, %{fault: fault}}}
  end

  defp request(method, path, body \\ "") do
    c = conn(method, "https://localhost" <> path, body)

    %{c | req_headers: [{"host", "localhost"} | c.req_headers]}
    |> put_req_header("content-type", "application/json")
  end

  test "agent namespace is admitted before browser session and rejects mixed credentials" do
    for {header, value} <- [
          {"authorization", "Bearer SECRET_SENTINEL"},
          {"cookie", "SECRET_SENTINEL"},
          {"origin", "https://localhost"},
          {"sec-fetch-site", "same-origin"},
          {"x-workharbor-context", "owner"}
        ] do
      result =
        request(:post, "/agent/v1/enroll", "{}")
        |> put_req_header(header, value)
        |> AgentIngress.call([])

      assert result.status == 403
      refute Map.has_key?(result.private, :plug_session)
      refute result.resp_body =~ "SECRET_SENTINEL"
    end
  end

  test "agent TLS authority exact path and query are checked before bodies" do
    for c <- [
          request(:post, "/agent/v1/enroll?code=SECRET_SENTINEL"),
          request(:post, "/agent/v1/enroll/"),
          %{request(:post, "/agent/v1/enroll") | scheme: :http},
          %{request(:post, "/agent/v1/enroll") | req_headers: [{"host", "elsewhere"}]}
        ] do
      result = AgentIngress.call(c, [])
      assert result.status in [400, 403]
      refute result.resp_body =~ "SECRET_SENTINEL"
    end
  end

  test "assigned JSON and owner principal never substitute for actual peer certificate" do
    for path <- ["/agent/v1/identity/renew", "/agent/v1/identity/activate", "/agent/v1/connect"] do
      c =
        request(if(path == "/agent/v1/connect", do: :get, else: :post), path, "{}")
        |> assign(:peer, %{authenticated: true})
        |> assign(:principal, %{owner_id: Ecto.UUID.generate()})

      result = AgentIngress.call(c, [])
      assert result.status == 401
      assert Jason.decode!(result.resp_body) == %{"error" => %{"code" => "unauthorized_host"}}
    end
  end

  test "enrollment body is closed strict and bounded with fixed redacted errors" do
    for raw <- [
          ~s({"schema_version":1,"schema_version":1}),
          ~s({"schema_version":1.0,"code":"SECRET_SENTINEL","csr":""}),
          ~s({"schema_version":1,"code":"SECRET_SENTINEL","csr":"","extra":true}),
          String.duplicate(" ", 16385)
        ] do
      result = AgentIngress.call(request(:post, "/agent/v1/enroll", raw), [])
      assert result.status == 400
      refute result.resp_body =~ "SECRET_SENTINEL"
    end
  end

  test "failed agent body validation preserves the consumed adapter state" do
    result =
      AgentIngress.call(
        request(:post, "/agent/v1/enroll", ~s({"schema_version":1,"extra":"SECRET_SENTINEL"})),
        []
      )

    assert elem(result.adapter, 1).req_body == ""
  end

  test "request read failure is a retryable 503, not a 400 refusal" do
    result = AgentIngress.call(faulty(:timeout), [])
    assert result.status == 503
    assert Jason.decode!(result.resp_body) == %{"error" => %{"code" => "request_read_failed"}}
  end

  test "an unexpected server exception is a retryable 503, not a 400 refusal" do
    defmodule RaisingAdapter do
      @moduledoc false
      def read_req_body(_, _), do: raise("boom")
      def send_resp(state, _status, _headers, body), do: {:ok, body, state}
    end

    c = request(:post, "/agent/v1/enroll", "{}")
    result = AgentIngress.call(%{c | adapter: {RaisingAdapter, nil}}, [])
    assert result.status == 503

    assert Jason.decode!(result.resp_body) == %{
             "error" => %{"code" => "host_service_unavailable"}
           }
  end

  test "post-activation confirmation: unavailable is 503, a genuine mismatch stays 400" do
    assert AgentIngress.confirmation_failure({:error, :unavailable}) ==
             {503, :host_service_unavailable}

    for other <- [{:error, :unauthorized_host}, {:ok, :stale}] do
      assert AgentIngress.confirmation_failure(other) == {400, :identity_activation_failed}
    end
  end

  test "activation failures: only final refusals have refusal codes, the rest are 503" do
    assert AgentIngress.activation_failure(:unauthorized_host) == {401, :unauthorized_host}

    assert AgentIngress.activation_failure(:predecessor_expired) ==
             {400, :identity_predecessor_expired}

    assert AgentIngress.activation_failure(:predecessor_missing) ==
             {400, :identity_predecessor_missing}

    for transient <- [:persistence_failed, :nested_transaction, :unavailable, :other] do
      assert AgentIngress.activation_failure(transient) == {503, :host_service_unavailable}
    end
  end
end
