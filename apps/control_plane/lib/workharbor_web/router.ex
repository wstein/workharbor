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

  pipeline :owner_live do
    plug :accepts, ["html"]
    plug :fetch_live_flash
    plug :put_root_layout, html: {WorkHarborWeb.Layouts, :root}
    plug :protect_from_forgery
    plug :owner_headers
  end

  pipeline :dashboard do
    plug RequireOwner, :dashboard_page
  end

  pipeline :host_enrollment do
    plug RequireOwner, :host_enrollment_issue
    plug :api_csrf
  end

  pipeline :host_revocation do
    plug RequireOwner, :host_revoke
    plug :api_csrf
  end

  pipeline :host_status do
    plug RequireOwner, :host_status
    plug :api_csrf
  end

  scope "/api/v1/hosts", WorkHarborWeb do
    pipe_through [:session, :host_enrollment]
    post "/enrollments", HostController, :issue, log: false
  end

  scope "/api/v1/hosts", WorkHarborWeb do
    pipe_through [:session, :host_revocation]
    post "/:id/revoke", HostController, :revoke, log: false
  end

  scope "/api/v1/hosts", WorkHarborWeb do
    pipe_through [:session, :host_status]
    get "/:id", HostController, :status, log: false
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
    get "/", AuthController, :owner_entry, log: false
    get "/owner/auth", AuthController, :page, log: false
  end

  scope "/", WorkHarborWeb do
    pipe_through [:session, :owner, :owner_live]

    live_session :owner, on_mount: WorkHarborWeb.OwnerAuth do
      live "/owner", OverviewLive, :index, log: false
    end
  end

  scope "/", WorkHarborWeb do
    pipe_through [:session, :dashboard]
    get "/dashboard", AuthController, :page
  end

  defp owner_headers(conn, _), do: WorkHarborWeb.Security.owner_headers(conn)

  defp cli_secret(conn, opts), do: RequireOwner.cli_secret(conn, opts)

  defp primary_admission(conn, _), do: RequireOwner.primary_admission(conn)
  defp api_csrf(conn, _), do: RequireOwner.cookie_csrf(conn)
end
