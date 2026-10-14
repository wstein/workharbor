# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.HostController do
  use WorkHarborWeb, :controller
  alias WorkHarbor.Hosts
  alias WorkHarborWeb.{Body, Security}

  def issue(conn, _) do
    parsed(conn, fn conn, body ->
      with %{"schema_version" => 1, "host_id" => host} <- body,
           true <-
             map_size(body) == 2 and (is_nil(host) or WorkHarbor.Foundation.Request.uuid?(host)),
           {:ok, issued} <- Hosts.issue_enrollment(conn.assigns.principal, host) do
        json(conn, Map.put(issued, :schema_version, 1))
      else
        _ -> refuse(conn, :host_enrollment_failed)
      end
    end)
  end

  def revoke(conn, %{"id" => host}) do
    parsed(conn, fn conn, body ->
      with %{"schema_version" => 1} <- body,
           true <- map_size(body) == 1,
           :ok <- Hosts.revoke(conn.assigns.principal, host) do
        json(conn, %{schema_version: 1, host_id: host, revoked: true})
      else
        _ -> refuse(conn, :host_revocation_failed)
      end
    end)
  end

  defp parsed(conn, callback) do
    case Body.parse(conn, :json) do
      {:ok, body, conn} -> callback.(conn, body)
      {:error, _, conn} -> refuse(conn, :invalid_request)
    end
  end

  defp refuse(conn, code),
    do: conn |> Plug.Conn.put_resp_header("connection", "close") |> Security.deny(400, code)

  def status(conn, %{"id" => host}) do
    case Hosts.status(conn.assigns.principal, host) do
      {:ok, status} ->
        connection =
          case WorkHarbor.HostConnections.host_state(host) do
            {:ok, "connected"} -> "connected"
            _ -> "unknown"
          end

        json(conn, Map.merge(status, %{schema_version: 1, connection: connection}))

      _ ->
        Security.deny(conn, 404, :not_found)
    end
  end
end
