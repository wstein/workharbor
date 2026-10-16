# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AgentIngress do
  @moduledoc "Raw agent transport boundary; browser credentials never prove a host identity."
  import Plug.Conn
  alias WorkHarbor.{Hosts, StrictJSON}
  alias WorkHarborWeb.Security

  @routes [
    {"POST", "/agent/v1/enroll"},
    {"POST", "/agent/v1/identity/renew"},
    {"POST", "/agent/v1/identity/activate"},
    {"GET", "/agent/v1/connect"}
  ]
  def init(opts), do: opts

  def call(conn, _) do
    conn = Security.call(conn, [])

    cond do
      conn.halted ->
        conn

      mixed_credentials?(conn) ->
        Security.deny(conn, 403, :forbidden)

      {conn.method, conn.request_path} not in @routes ->
        Security.deny(conn, 400, :invalid_request)

      conn.request_path == "/agent/v1/enroll" ->
        enrollment(conn)

      true ->
        authenticated(conn)
    end
  rescue
    _ -> unavailable(conn, :host_service_unavailable)
  end

  defp mixed_credentials?(conn),
    do:
      Enum.any?(conn.req_headers, fn {name, _} ->
        name in ["authorization", "cookie", "origin", "x-workharbor-context"] or
          String.starts_with?(name, "sec-fetch-")
      end)

  defp authenticated(conn) do
    mode = if conn.request_path == "/agent/v1/identity/activate", do: :activation, else: :active
    # Only the server adapter's verified TLS peer certificate is consulted.
    cert =
      if elem(conn.adapter, 0) == Bandit.Adapter, do: get_peer_data(conn)[:ssl_cert], else: nil

    case Hosts.resolve_identity(cert, mode) do
      {:ok, peer} -> dispatch(conn, peer)
      {:error, :unavailable} -> unavailable(conn, :host_service_unavailable)
      _ -> Security.deny(conn, 401, :unauthorized_host)
    end
  end

  defp enrollment(conn) do
    parsed(conn, ~w(schema_version code csr), fn conn, body ->
      with code when is_binary(code) <- body["code"],
           {:ok, csr} <- der(body["csr"]),
           {:ok, response} <- Hosts.redeem(code, csr) do
        json(conn, response)
      else
        _ -> refuse_body(conn, :invalid_enrollment)
      end
    end)
  end

  @doc false
  # Final refusals carry their own code; every other activation error is a retryable 503.
  def activation_failure(:unauthorized_host), do: {401, :unauthorized_host}
  def activation_failure(:predecessor_expired), do: {400, :identity_predecessor_expired}
  def activation_failure(:predecessor_missing), do: {400, :identity_predecessor_missing}
  def activation_failure(_), do: {503, :host_service_unavailable}

  @doc false
  # After a successful activation the new identity must resolve as current: an
  # unavailable database or issuer is retryable, a genuine mismatch is not final either.
  def confirmation_failure({:error, :unavailable}), do: {503, :host_service_unavailable}
  def confirmation_failure(_), do: {400, :identity_activation_failed}

  defp dispatch(%{request_path: "/agent/v1/identity/renew"} = conn, peer) do
    parsed(conn, ~w(schema_version csr), fn conn, body ->
      with {:ok, csr} <- der(body["csr"]), {:ok, response} <- Hosts.renew(peer, csr) do
        json(conn, response)
      else
        _ -> refuse_body(conn, :identity_renewal_failed)
      end
    end)
  end

  defp dispatch(%{request_path: "/agent/v1/identity/activate"} = conn, peer) do
    parsed(conn, ~w(schema_version), fn conn, _ ->
      with {:activate, :ok} <- {:activate, Hosts.activate(peer)},
           {:issuer, {:ok, issuer}} <- {:issuer, Hosts.issuer_for(peer.certificate)},
           {:confirm, {:ok, current}} when current === peer <-
             {:confirm, Hosts.resolve_identity(peer.certificate, :active)} do
        json(conn, %{
          "schema_version" => 1,
          "owner_id" => peer.owner_id,
          "host_id" => peer.host_id,
          "identity_id" => peer.identity_id,
          "certificate" => Base.encode64(peer.certificate),
          "issuer_certificate" => Base.encode64(issuer.certificate),
          "expires_at" => peer.expires_at
        })
      else
        {:activate, {:error, reason}} ->
          {status, code} = activation_failure(reason)
          Security.deny(conn, status, code)

        {:issuer, _} ->
          unavailable(conn, :host_service_unavailable)

        {:confirm, result} ->
          {status, code} = confirmation_failure(result)
          Security.deny(conn, status, code)
      end
    end)
  end

  defp dispatch(conn, peer) do
    if upgrade?(conn) do
      conn
      |> WebSockAdapter.upgrade(WorkHarborWeb.AgentSocket, peer,
        compress: false,
        max_frame_size: 4110,
        max_fragmented_message_size: 4096,
        timeout: 30000
      )
      |> halt()
    else
      Security.deny(conn, 400, :invalid_request)
    end
  end

  defp parsed(conn, keys, callback) do
    if get_req_header(conn, "content-type") == ["application/json"] do
      case read_body(conn, length: 16384, read_length: 16384, read_timeout: 5000) do
        {:ok, raw, conn} when byte_size(raw) <= 16384 ->
          case StrictJSON.decode_object(raw) do
            {:ok, body} ->
              if Enum.sort(Map.keys(body)) == Enum.sort(keys) and body["schema_version"] === 1,
                do: callback.(conn, body),
                else: refuse_body(conn, :invalid_request)

            _ ->
              refuse_body(conn, :invalid_request)
          end

        {status, _, conn} when status in [:more, :ok] ->
          refuse_body(conn, :invalid_request)

        {:error, _} ->
          unavailable(conn, :request_read_failed)
      end
    else
      refuse_body(conn, :invalid_request)
    end
  end

  # A retryable server or read fault, never a refusal of the caller's identity.
  defp unavailable(conn, code),
    do: conn |> put_resp_header("connection", "close") |> Security.deny(503, code)

  defp refuse_body(conn, code),
    do: conn |> put_resp_header("connection", "close") |> Security.deny(400, code)

  defp der(value) when is_binary(value) and byte_size(value) <= 5464 do
    with {:ok, bytes} <- Base.decode64(value),
         true <- byte_size(bytes) <= 4096 and Base.encode64(bytes) == value do
      {:ok, bytes}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp der(_), do: {:error, :invalid_request}

  defp json(conn, body),
    do:
      conn
      |> put_resp_content_type("application/json")
      |> send_resp(200, Jason.encode!(body))
      |> halt()

  defp upgrade?(conn) do
    get_req_header(conn, "sec-websocket-version") == ["13"] and
      get_req_header(conn, "sec-websocket-extensions") == [] and
      get_req_header(conn, "sec-websocket-protocol") == [] and
      get_req_header(conn, "connection") |> token?("upgrade") and
      get_req_header(conn, "upgrade") |> token?("websocket") and
      case get_req_header(conn, "sec-websocket-key") do
        [key] when byte_size(key) == 24 ->
          case Base.decode64(key) do
            {:ok, bytes} -> byte_size(bytes) == 16 and Base.encode64(bytes) == key
            _ -> false
          end

        _ ->
          false
      end
  end

  defp token?([value], expected) when byte_size(value) <= 128,
    do: expected in (value |> String.downcase() |> Plug.Conn.Utils.list())

  defp token?(_, _), do: false
end
