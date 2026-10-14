# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.Router do
  use WorkHarborWeb, :router
  alias WorkHarborWeb.Plugs.RequireOwner

  pipeline :session do
    plug :fetch_session
  end

  pipeline :primary do
    plug :primary_admission
  end

  pipeline :read do
    plug RequireOwner, :status
    plug :api_csrf
  end

  pipeline :logout do
    plug RequireOwner, :logout
    plug :api_csrf
  end

  pipeline :revoke do
    plug RequireOwner, :revoke
    plug :api_csrf
  end

  pipeline :issue do
    plug RequireOwner, :issue_dashboard
    plug :api_csrf
  end

  pipeline :issue_secret do
    plug RequireOwner, :issue_dashboard
    plug :cli_secret
  end

  pipeline :browser_logout do
    plug RequireOwner, :logout
  end

  pipeline :browser_issue do
    plug RequireOwner, :issue_dashboard
  end

  pipeline :owner do
    plug RequireOwner, :owner_page
  end

  pipeline :dashboard do
    plug RequireOwner, :dashboard_page
  end

  scope "/", WorkHarborWeb do
    pipe_through :session
    get "/login/owner", AuthController, :form, log: false
    get "/login/dashboard", AuthController, :form, log: false
    get "/login/read-token", AuthController, :form, log: false
    post "/login/owner", AuthController, :submit, log: false
    post "/login/dashboard", AuthController, :submit, log: false
    post "/login/read-token", AuthController, :submit, log: false
    post "/login/dashboard/clear", AuthController, :clear, log: false
  end

  scope "/api/v1/auth", WorkHarborWeb do
    pipe_through [:session, :primary]
    post "/login", AuthController, :login, log: false
  end

  scope "/api/v1/auth", WorkHarborWeb do
    pipe_through [:session, :read]
    get "/status", AuthController, :status, log: false
  end

  scope "/api/v1/auth", WorkHarborWeb do
    pipe_through [:session, :logout]
    post "/logout", AuthController, :logout, log: false
  end

  scope "/api/v1/auth", WorkHarborWeb do
    pipe_through [:session, :revoke]
    post "/revoke", AuthController, :revoke, log: false
  end

  scope "/api/v1/auth", WorkHarborWeb do
    pipe_through [:session, :issue_secret]
    post "/dashboard-credentials", AuthController, :issue, log: false
  end

  scope "/api/v1/auth", WorkHarborWeb do
    pipe_through [:session, :issue]
    post "/dashboard-install", AuthController, :install, log: false
  end

  scope "/owner", WorkHarborWeb do
    pipe_through [:session, :browser_logout]
    post "/logout", AuthController, :browser_logout, log: false
  end

  scope "/owner", WorkHarborWeb do
    pipe_through [:session, :browser_issue]
    post "/dashboard", AuthController, :browser_dashboard, log: false
  end

  scope "/", WorkHarborWeb do
    pipe_through [:session, :owner]
    get "/", AuthController, :page
    get "/owner", AuthController, :page
  end

  scope "/", WorkHarborWeb do
    pipe_through [:session, :dashboard]
    get "/dashboard", AuthController, :page
  end

  defp cli_secret(conn, opts), do: RequireOwner.cli_secret(conn, opts)

  defp primary_admission(conn, _), do: RequireOwner.primary_admission(conn)
  defp api_csrf(conn, _), do: RequireOwner.cookie_csrf(conn)
end
