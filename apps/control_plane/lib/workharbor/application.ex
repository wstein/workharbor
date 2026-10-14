# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Application do
  # See https://elixir.hexdocs.pm/Application.html
  # for more information on OTP Applications
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    identity = Application.get_env(:workharbor, :host_identity)

    {:ok, endpoint} =
      WorkHarborWeb.RuntimeSecurity.identity_config(
        Application.fetch_env!(:workharbor, WorkHarborWeb.Endpoint),
        identity
      )

    Application.put_env(:workharbor, WorkHarborWeb.Endpoint, endpoint)

    WorkHarborWeb.RuntimeSecurity.validate!(
      endpoint,
      Application.fetch_env!(:workharbor, :owner_http)
    )

    if identity, do: :ok = WorkHarborWeb.AgentLogFilter.install()

    children = [
      WorkHarborWeb.Telemetry,
      WorkHarbor.Repo,
      {Ecto.Migrator,
       repos: Application.fetch_env!(:workharbor, :ecto_repos), skip: skip_migrations?()},
      {Phoenix.PubSub, name: WorkHarbor.PubSub},
      # Liveness is initialized before the listener when host identity is configured.
      # Start a worker by calling: WorkHarbor.Worker.start_link(arg)
      # {WorkHarbor.Worker, arg},
      # Start to serve requests, typically the last entry
      WorkHarborWeb.Endpoint
    ]

    children =
      if identity, do: List.insert_at(children, -2, WorkHarbor.HostConnections), else: children

    # See https://elixir.hexdocs.pm/Supervisor.html
    # for other strategies and supported options
    opts = [strategy: :one_for_one, name: WorkHarbor.Supervisor]
    Supervisor.start_link(children, opts)
  end

  # Tell Phoenix to update the endpoint configuration
  # whenever the application is updated.
  @impl true
  def config_change(changed, _new, removed) do
    WorkHarborWeb.Endpoint.config_change(changed, removed)
    :ok
  end

  defp skip_migrations?() do
    # By default, sqlite migrations are run when using a release
    System.get_env("RELEASE_NAME") == nil
  end
end
