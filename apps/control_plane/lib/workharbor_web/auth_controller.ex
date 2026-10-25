# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AuthController do
  use WorkHarborWeb, :controller
  import Plug.Conn
  alias WorkHarbor.Accounts
  alias WorkHarborWeb.{Security, BrowserSession, Body}

  def login(conn, _) do
    case Body.parse(conn, :json) do
      {:ok, %{"schema_version" => 1, "owner_id" => owner, "owner_secret" => secret} = body, conn}
      when map_size(body) == 3 ->
        case Accounts.login(owner, secret, "cli_owner") do
          {:ok, token, p} -> json(conn, %{token: token, credential: Accounts.status(p)})
          {:error, e} -> error(conn, e)
        end

      {:ok, _, conn} ->
        error(conn, :invalid_request)

      {:error, e, conn} ->
        error(conn, e)
    end
  end

  def status(conn, _), do: json(conn, %{credential: Accounts.status(conn.assigns.principal)})

  def logout(conn, _) do
    parsed(conn, :json, fn conn, body ->
      with true <- map_size(body) == 0,
           :ok <- Accounts.revoke(conn.assigns.principal, conn.assigns.principal.credential_id) do
        conn =
          if conn.assigns.auth_channel == :cookie,
            do: configure_session(conn, drop: true),
            else: conn

        json(conn, %{ok: true})
      else
        {:error, e} -> error(conn, e)
        _ -> error(conn, :invalid_request)
      end
    end)
  end

  def issue(conn, _), do: perform_issue(conn, false)
  def install(conn, _), do: perform_issue(conn, true)

  defp perform_issue(conn, install?) do
    parsed(conn, :json, fn conn, body ->
      with true <- map_size(body) == 0,
           {:ok, token, p} <- Accounts.issue_dashboard(conn.assigns.principal) do
        if install?,
          do: installed(conn, token, p),
          else: json(conn, %{token: token, credential: Accounts.status(p)})
      else
        {:error, e} -> error(conn, e)
        _ -> error(conn, :invalid_request)
      end
    end)
  end

  def revoke(conn, _) do
    parsed(conn, :json, fn conn, body ->
      with %{"credential_id" => id} when map_size(body) == 1 <- body,
           :ok <- Accounts.revoke(conn.assigns.principal, id) do
        json(conn, %{ok: true})
      else
        {:error, e} -> error(conn, e)
        _ -> error(conn, :invalid_request)
      end
    end)
  end

  def form(conn, _) do
    conn = BrowserSession.initialize_preauth(conn)
    conn = Plug.CSRFProtection.call(conn, Plug.CSRFProtection.init([]))
    token = Plug.CSRFProtection.get_csrf_token()

    fields =
      if conn.request_path == "/login/read-token",
        do:
          ~s(<label>Read token<input type="password" name="dashboard_token" autocomplete="off" required></label>),
        else:
          ~s(<label>Owner ID<input name="owner_id" required></label><label>Owner secret<input type="password" name="owner_secret" autocomplete="off" required></label>)

    html(
      conn,
      ~s(<!doctype html><html><meta charset="utf-8"><title>WorkHarbor sign in</title><h1>WorkHarbor sign in</h1><form method="post" action="#{conn.request_path}"><input type="hidden" name="_csrf_token" value="#{token}">#{fields}<button>Sign in</button></form></html>)
    )
  end

  def submit(conn, _) do
    fields =
      if conn.request_path == "/login/read-token",
        do: ["dashboard_token", "_csrf_token"],
        else: ["owner_id", "owner_secret", "_csrf_token"]

    if Security.own_origin?(conn) and BrowserSession.valid_preauth?(conn) do
      parsed(conn, {:form, fields}, fn conn, body ->
        with true <-
               byte_size(body["_csrf_token"]) == 56 and
                 BrowserSession.selected_csrf?(conn, body["_csrf_token"]),
             {:ok, token, p} <- browser_login(conn, body) do
          conn
          |> configure_session(drop: true)
          |> BrowserSession.install(conn.private.auth_target, token, p.expires_at)
          |> redirect_to(if(conn.private.auth_target == :owner, do: "/owner", else: "/dashboard"))
        else
          {:error, e} -> error(conn, e)
          _ -> error(conn, :forbidden)
        end
      end)
    else
      error(conn, :forbidden)
    end
  end

  defp browser_login(%{request_path: "/login/read-token"}, body) do
    case Accounts.authenticate(body["dashboard_token"], "dashboard") do
      {:ok, p} -> {:ok, body["dashboard_token"], p}
      e -> e
    end
  end

  defp browser_login(conn, body),
    do:
      Accounts.login(
        body["owner_id"],
        body["owner_secret"],
        if(conn.private.auth_target == :owner, do: "owner_browser", else: "dashboard")
      )

  def owner_entry(conn, _), do: redirect_to(conn, "/owner")

  def page(conn, _) do
    conn = Plug.CSRFProtection.call(conn, Plug.CSRFProtection.init([]))
    token = Plug.CSRFProtection.get_csrf_token()
    owner? = conn.private.auth_target == :owner
    title = if owner?, do: "Owner management", else: "Read-only dashboard"

    controls =
      if owner? do
        control_form("/owner/dashboard", token, "Open read-only dashboard") <>
          control_form("/owner/logout", token, "Sign out of owner management")
      else
        control_form("/login/dashboard/clear", token, "Clear dashboard sign-in")
      end

    html(
      conn,
      ~s(<!doctype html><html><meta charset="utf-8"><title>WorkHarbor</title><h1>#{title}</h1>#{controls}</html>)
    )
  end

  def clear(conn, _),
    do:
      browser_form(conn, fn conn ->
        conn |> configure_session(drop: true) |> redirect_to("/login/read-token")
      end)

  def browser_logout(conn, _) do
    browser_form(conn, fn conn ->
      case Accounts.revoke(conn.assigns.principal, conn.assigns.principal.credential_id) do
        :ok -> conn |> configure_session(drop: true) |> redirect_to("/login/owner")
        {:error, e} -> error(conn, e)
      end
    end)
  end

  def browser_dashboard(conn, _) do
    browser_form(conn, fn conn ->
      case Accounts.issue_dashboard(conn.assigns.principal) do
        {:ok, token, p} -> installed(conn, token, p)
        {:error, e} -> error(conn, e)
      end
    end)
  end

  defp browser_form(conn, work) do
    if Security.own_origin?(conn) do
      parsed(conn, {:form, ["_csrf_token"]}, fn conn, body ->
        if BrowserSession.selected_csrf?(conn, body["_csrf_token"]),
          do: work.(conn),
          else: error(conn, :forbidden)
      end)
    else
      error(conn, :forbidden)
    end
  end

  defp parsed(conn, type, work) do
    case Body.parse(conn, type) do
      {:ok, body, updated} -> work.(updated, body)
      {:error, e, updated} -> error(updated, e)
    end
  end

  defp installed(conn, token, p),
    do:
      conn |> BrowserSession.install(:dashboard, token, p.expires_at) |> redirect_to("/dashboard")

  defp redirect_to(conn, path),
    do: conn |> put_resp_header("location", path) |> send_resp(303, "")

  defp control_form(path, csrf, label),
    do:
      ~s(<form method="post" action="#{path}"><input type="hidden" name="_csrf_token" value="#{csrf}"><button>#{label}</button></form>)

  def error(conn, e),
    do:
      Security.deny(
        conn,
        case e do
          :unauthenticated -> 401
          :forbidden -> 403
          :not_found -> 404
          :input_limit_exceeded -> 413
          :unsupported_media_type -> 415
          :persistence_failed -> 503
          _ -> 400
        end,
        e
      )
end
