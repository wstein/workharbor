# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.Plugs.RequireOwner do
  @moduledoc false
  import Plug.Conn
  alias WorkHarbor.{Accounts}
  alias WorkHarborWeb.{Security, BrowserSession}
  def init(opts), do: opts

  def call(conn, action) do
    conn = fetch_session(conn)

    with {:ok, p, channel} <- principal(conn),
         :ok <- Accounts.authorize(p, action),
         :ok <- channel_admission(conn, channel, action) do
      conn |> assign(:principal, p) |> assign(:auth_channel, channel)
    else
      {:error, :forbidden} -> Security.deny(conn, 403, :forbidden)
      {:error, :context} -> Security.deny(conn, 400, :invalid_context)
      _ -> Security.deny(conn, 401, :unauthenticated)
    end
  end

  def principal(conn) do
    cookie? = BrowserSession.auth_cookies?(conn)

    case get_req_header(conn, "authorization") do
      ["Bearer " <> token] ->
        with {:ok, p} <- Accounts.authenticate(token),
             true <- p.kind in ["cli_owner", "dashboard"] do
          if cookie?, do: known_mixed(p), else: {:ok, p, :bearer}
        else
          _ -> {:error, :unauthenticated}
        end

      [] ->
        target = conn.private.auth_target

        cond do
          is_nil(target) and cookie? ->
            {:error, :context}

          is_nil(target) ->
            {:error, :unauthenticated}

          true ->
            case Accounts.authenticate(
                   get_session(conn, "credential"),
                   if(target == :owner, do: "owner_browser", else: "dashboard")
                 ) do
              {:ok, p} -> {:ok, p, :cookie}
              e -> e
            end
        end

      _ ->
        {:error, :unauthenticated}
    end
  end

  def primary_admission(conn) do
    conn = fetch_session(conn)

    case principal(conn) do
      {:ok, %{kind: "dashboard"} = p, _} ->
        Accounts.authorize(p, :primary_login)
        Security.deny(conn, 403, :forbidden)

      {:error, :forbidden} ->
        Security.deny(conn, 403, :forbidden)

      _ ->
        if get_req_header(conn, "authorization") != [] or BrowserSession.auth_cookies?(conn) or
             Enum.any?(
               ["origin", "sec-fetch-site", "x-workharbor-context"],
               &(get_req_header(conn, &1) != [])
             ), do: Security.deny(conn, 400, :invalid_channel), else: conn
    end
  end

  def cli_secret(conn, _) do
    if conn.assigns.auth_channel == :bearer and conn.assigns.principal.kind == "cli_owner" do
      conn
    else
      Accounts.deny(conn.assigns.principal, :issue_dashboard)
      Security.deny(conn, 403, :forbidden)
    end
  end

  def cookie_csrf(conn) do
    if conn.assigns[:auth_channel] == :cookie do
      origin = Security.own_origin?(conn)

      allowed =
        origin or
          (conn.method in ["GET", "HEAD"] and get_req_header(conn, "origin") == [] and
             get_req_header(conn, "sec-fetch-site") == ["same-origin"])

      token =
        case get_req_header(conn, "x-csrf-token") do
          [v] -> v
          _ -> nil
        end

      if allowed and BrowserSession.selected_csrf?(conn, token),
        do: conn,
        else: Security.deny(conn, 403, :forbidden)
    else
      conn
    end
  end

  defp known_mixed(%{kind: "dashboard"} = p), do: Accounts.authorize(p, :primary_login)
  defp known_mixed(_), do: {:error, :unauthenticated}

  defp channel_admission(conn, :bearer, _) do
    if not BrowserSession.auth_cookies?(conn) and
         Enum.all?(
           ["origin", "sec-fetch-site", "x-workharbor-context"],
           &(get_req_header(conn, &1) == [])
         ), do: :ok, else: {:error, :forbidden}
  end

  defp channel_admission(conn, :cookie, action) when action in [:owner_page, :dashboard_page] do
    if get_req_header(conn, "sec-fetch-site") in [[], ["none"], ["same-origin"]],
      do: :ok,
      else: {:error, :forbidden}
  end

  defp channel_admission(_, :cookie, _), do: :ok
end
